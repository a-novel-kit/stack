// Package repocfg computes a repository's desired GitHub configuration, from its
// settings and security to its rulesets, environments and managed workflow
// files, as a Plan of API operations.
//
// The desired state follows from three inputs: the class preset
// (classes/<class>.yaml), the org profile (orgs/<org>.yaml), and what Discover
// reads from the repository's workflows. A repos/<org>_<repo>.yaml override
// replaces the class preset. The templates are plain YAML, and an on-disk copy,
// under the default stack or $REPO_CONFIG_DIR, wins over the embedded one, so an
// edit needs no rebuild.
package repocfg
