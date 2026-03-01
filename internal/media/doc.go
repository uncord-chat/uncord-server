// Package media abstracts file storage behind the StorageProvider interface. Implementations handle Put, Get, Delete,
// and URL generation for stored files. The package defines allowlists for permitted content types and thumbnail-eligible
// image types, and provides MIME normalisation and file extension helpers.
package media
