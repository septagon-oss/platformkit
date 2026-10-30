// Package httpx builds the HTTP API and validates its declared routes before
// serving. Its surface and middleware chain are selected at mount time; tenant
// requests use a lazy transaction and buffered response so a refusal or failed
// transaction cannot expose a partial result. See README.md for surface contracts.
package httpx
