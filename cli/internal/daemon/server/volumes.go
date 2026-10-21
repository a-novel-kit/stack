package server

import (
	"context"

	"connectrpc.com/connect/v2"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	"github.com/a-novel-kit/stack/cli/internal/daemon/volumes"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// ListVolumes returns one read-only Volume row per compose-declared volume on
// the service, with its size and backup count.
func (s *Server) ListVolumes(_ context.Context, req *anovelv1.ListVolumesRequest) (*anovelv1.ListVolumesResponse, error) {
	svc, err := s.findService(req.GetStack(), req.GetService())
	if err != nil {
		return nil, err
	}
	out := &anovelv1.ListVolumesResponse{}
	for _, v := range volumes.List(svc) {
		out.Volumes = append(out.Volumes, volumeToProto(v))
	}
	return out, nil
}

// BackupVolume writes a tar.zst snapshot per volume. It refuses while the
// service is up, unless --force cascade-kills it first.
func (s *Server) BackupVolume(ctx context.Context, req *anovelv1.BackupVolumeRequest) (*anovelv1.BackupVolumeResponse, error) {
	svc, err := s.findService(req.GetStack(), req.GetService())
	if err != nil {
		return nil, err
	}
	if err := s.ensureServiceDown(ctx, svc, req.GetForce(), "backup"); err != nil {
		return nil, err
	}
	paths, err := volumes.Backup(svc, req.GetTag())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err.Error()).WithCause(err)
	}
	return &anovelv1.BackupVolumeResponse{ArchivePaths: paths}, nil
}

// RestoreVolume replaces each volume from its matching backup. It refuses while
// the service is up.
func (s *Server) RestoreVolume(ctx context.Context, req *anovelv1.RestoreVolumeRequest) (*anovelv1.RestoreVolumeResponse, error) {
	svc, err := s.findService(req.GetStack(), req.GetService())
	if err != nil {
		return nil, err
	}
	if err := s.ensureServiceDown(ctx, svc, req.GetForce(), "restore"); err != nil {
		return nil, err
	}
	restored, err := volumes.Restore(svc, req.GetFrom())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err.Error()).WithCause(err)
	}
	return &anovelv1.RestoreVolumeResponse{RestoredVolumes: restored}, nil
}

// ClearVolume destroys the service's volumes, backing them up first unless
// --no-backup. It refuses while the service is up.
func (s *Server) ClearVolume(ctx context.Context, req *anovelv1.ClearVolumeRequest) (*anovelv1.ClearVolumeResponse, error) {
	svc, err := s.findService(req.GetStack(), req.GetService())
	if err != nil {
		return nil, err
	}
	if err := s.ensureServiceDown(ctx, svc, req.GetForce(), "clear"); err != nil {
		return nil, err
	}
	cleared, err := volumes.Clear(svc, req.GetNoBackup())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err.Error()).WithCause(err)
	}
	return &anovelv1.ClearVolumeResponse{ClearedVolumes: cleared}, nil
}

// ensureServiceDown is the pre-check every destructive volume operation shares.
// It refuses with a hint while any target or infra is up; with force it
// cascade-kills both first.
func (s *Server) ensureServiceDown(ctx context.Context, svc *discovery.Service, force bool, op string) error {
	var running []string
	for _, inst := range s.runner.AllInstances() {
		if inst.Stack == svc.Stack && inst.Service == svc.Name && inst.Phase != anovelv1.Phase_PHASE_TERMINATED {
			running = append(running, inst.Target)
		}
	}
	sess, _ := s.runner.InfraSession(svc.Stack, svc.Name)
	if len(running) == 0 && !sess.Up {
		return nil // all clear
	}
	if !force {
		return connect.Errorf(connect.CodeFailedPrecondition,
			"refusing to %s while %s/%s is up — kill targets + infra first (or pass --force):\n  running targets: %v\n  infra session up: %v",
			op, svc.Stack, svc.Name, running, sess.Up)
	}
	// A forced KillInfra cascades through the targets and the infra alike.
	if err := s.runner.KillInfra(ctx, svc.Stack, svc.Name, true /* force */); err != nil {
		return connect.Errorf(connect.CodeInternal,
			"force-stop before %s: %v", op, err).WithCause(err)
	}
	return nil
}
