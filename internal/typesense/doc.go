// Package typesense provides an HTTP client for indexing messages in Typesense. The Indexer retries transient failures
// (5xx responses) with a fixed delay. Indexing operations are best-effort and designed to run in background workers.
package typesense
