package hostenv

import (
	"cmp"
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
)

// Localhost is the host every published port is reached on.
const Localhost = "localhost"

// postgresDefault is the user, password and database of a local Postgres that
// sets none.
const postgresDefault = "postgres"

// Listen binds a free ephemeral TCP port on the loopback interface and returns
// the listener holding it, with the port. Closing the listener frees the port
// for the process meant to bind it. Another process can claim it in between,
// which local development accepts.
func Listen(ctx context.Context) (net.Listener, int, error) {
	listener, err := new(net.ListenConfig).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return nil, 0, err
	}

	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		_ = listener.Close()
		return nil, 0, fmt.Errorf("unexpected listener address type %T", listener.Addr())
	}
	return listener, addr.Port, nil
}

// URL is the <base>_URL value for what a compose file publishes as
// <base>_PORT on port: the schemeless host:port grpc-go clients take for gRPC,
// an http:// URL otherwise.
func URL(base string, port int) string {
	hostPort := net.JoinHostPort(Localhost, strconv.Itoa(port))
	if base == "GRPC" {
		return hostPort
	}
	return "http://" + hostPort
}

// Postgres returns the settings that point a host process at a Postgres on
// port: the discrete POSTGRES_* settings services read, and the POSTGRES_DSN
// URL a command such as psql takes. An empty user, password or database falls
// back to postgres; the DSN percent-escapes the others. The local database
// serves no TLS.
func Postgres(user, password, database string, port int) map[string]string {
	user = cmp.Or(user, postgresDefault)
	password = cmp.Or(password, postgresDefault)
	database = cmp.Or(database, postgresDefault)
	hostPort := strconv.Itoa(port)

	dsn := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(user, password),
		Host:     net.JoinHostPort(Localhost, hostPort),
		Path:     "/" + database,
		RawQuery: "sslmode=disable",
	}
	return map[string]string{
		"POSTGRES_DSN":      dsn.String(),
		"POSTGRES_HOST":     Localhost,
		"POSTGRES_PORT":     hostPort,
		"POSTGRES_USER":     user,
		"POSTGRES_PASSWORD": password,
		"POSTGRES_DATABASE": database,
	}
}
