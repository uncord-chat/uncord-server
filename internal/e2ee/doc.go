// Package e2ee provides server-side key distribution and ciphertext relay for end-to-end encrypted direct messages. The
// server stores public key material (identity keys, signed pre-keys, one-time pre-keys) and delivers it to clients on
// request so they can establish pairwise X3DH sessions. It never sees plaintext message content. All encryption and
// decryption is performed client-side.
package e2ee
