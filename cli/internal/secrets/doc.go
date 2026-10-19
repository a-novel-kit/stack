// Package secrets is the a-novel CLI's local, encrypted secrets manager.
//
// A developer or an AI agent runs `a-novel test`, `run` or `ui` with API secrets,
// such as OPENAI_API_KEY, injected into the child process's environment only. A
// secret is never printed, logged, committed, or passed as a CLI argument. The
// manager prevents accidental exposure; it does not defend against a local
// attacker who can already read the user's home directory.
//
//   - A 32-byte local key from crypto/rand lives at SecretsRoot()/key, mode 0600
//     inside a 0700 directory. It is generated once and never overwritten.
//   - The store at SecretsRoot()/store.enc holds a flat JSON map {secretID: value}
//     encrypted as a whole with AES-256-GCM. Each write uses a fresh random
//     12-byte nonce, stored as `nonce || ciphertext`, and lands atomically
//     (temp file and rename) at mode 0600.
//   - No error embeds a secret value, and a Store is never formatted with %v.
//
// The crypto is the Go standard library's alone.
package secrets
