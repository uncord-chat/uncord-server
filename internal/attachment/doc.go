// Package attachment manages file attachments associated with messages. Attachments are created in a pending state and
// linked to a message in a separate step, allowing uploads to complete before the message is sent. The Repository
// interface provides methods for creation, linkage, thumbnail storage, and orphan cleanup.
package attachment
