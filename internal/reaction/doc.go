// Package reaction provides domain types for message reactions. Both custom emoji (referenced by UUID) and Unicode emoji
// (stored as strings) are supported. The Repository interface defines operations for adding, removing, listing, and
// summarising reactions, including per-user tracking to prevent duplicate reactions on the same message.
package reaction
