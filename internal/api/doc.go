// Package api provides HTTP handlers for the Uncord REST API. Each handler is a thin adapter that parses a protocol
// request type from the request body, delegates business logic to the service or repository layer, and writes a protocol
// response type using the httputil envelope helpers. Handlers receive fiber.Ctx by value and map service errors to HTTP
// status codes via dedicated error mapping functions.
package api
