package workbench

import (
	"fmt"

	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// writeCapabilities derives the write-capability descriptor for the open
// service: WriteCapabilitiesProvider when present, otherwise the same
// descriptor from in-process type assertions. Product names are display
// only; every browse write action dispatches on this descriptor.
func (m Model) writeCapabilities() sharedsql.WriteCapabilities {
	if provider, ok := m.Database.(sharedsql.WriteCapabilitiesProvider); ok {
		return provider.WriteCapabilities()
	}
	capabilities := sharedsql.WriteCapabilities{}
	if _, ok := m.Database.(sharedsql.RowWriter); ok {
		capabilities.RowWriter = true
	}
	if _, ok := m.Database.(sharedsql.DocumentWriter); ok {
		capabilities.Document = &sharedsql.DocumentWriteCapability{Text: true}
	}
	return capabilities
}

// rowWriter returns the open service's RowWriter adapter, or nil when the
// service does not support row writes.
func (m Model) rowWriter() sharedsql.RowWriter {
	if writer, ok := m.Database.(sharedsql.RowWriter); ok {
		return writer
	}
	return nil
}

// documentCapability returns the open service's document write capability,
// or nil when it has none.
func (m Model) documentCapability() *sharedsql.DocumentWriteCapability {
	return m.writeCapabilities().Document
}

// documentReader returns the open service's DocumentReader adapter, or nil.
func (m Model) documentReader() sharedsql.DocumentReader {
	if reader, ok := m.Database.(sharedsql.DocumentReader); ok {
		return reader
	}
	return nil
}

// documentWriter returns the open service's DocumentWriter adapter, or nil.
func (m Model) documentWriter() sharedsql.DocumentWriter {
	if writer, ok := m.Database.(sharedsql.DocumentWriter); ok {
		return writer
	}
	return nil
}

// rowWriteUnsupportedError names the failure for a stale row/document
// action on a service that lacks the capability.
func (m Model) rowWriteUnsupportedError() error {
	return fmt.Errorf("row editing is not supported by %s", m.databaseInfo.Product)
}

// browseWriteAvailable reports whether any browse row/document write action
// can run on the open service.
func (m Model) browseWriteAvailable() bool {
	capabilities := m.writeCapabilities()
	return capabilities.RowWriter || capabilities.Document != nil
}

// browseDocumentIdentity returns the selected browse row's stable document
// identity, or nil when the row has none.
func (m Model) browseDocumentIdentity() *sharedsql.DocumentPayload {
	row := m.browse.table.Cursor()
	if row < 0 || row >= len(m.browse.result.DocumentIDs) {
		return nil
	}
	identity := m.browse.result.DocumentIDs[row]
	if identity.Format == "" && len(identity.Data) == 0 {
		return nil
	}
	return &identity
}

// browseRowMenuOptions builds the browse-tab context menu for the open
// service's write capabilities: SQL row actions for RowWriter services,
// document actions for document stores, nothing for stores with neither.
func (m Model) browseRowMenuOptions() []menuOption {
	capabilities := m.writeCapabilities()
	if capabilities.RowWriter {
		return []menuOption{
			{label: "Insert row", action: "insert_row", keys: "a"},
			{label: "Copy cell", action: "copy_cell", keys: "y"},
			{label: "Edit cell", action: "edit_cell", keys: "i"},
			{label: "Edit row", action: "edit_row", keys: "enter"},
			{label: "Delete row", action: "delete_row", keys: "d"},
		}
	}
	if capabilities.Document != nil {
		if !capabilities.Document.Text {
			// A non-text document capability can only delete by identity.
			return []menuOption{
				{label: "Delete document", action: "delete_row", keys: "d"},
			}
		}
		return []menuOption{
			{label: "Insert document", action: "insert_row", keys: "a"},
			{label: "Copy cell", action: "copy_cell", keys: "y"},
			{label: "Edit document", action: "edit_row", keys: "enter"},
			{label: "Delete document", action: "delete_row", keys: "d"},
		}
	}
	// Stores with neither capability keep the read-only cell action.
	return []menuOption{
		{label: "Copy cell", action: "copy_cell", keys: "y"},
	}
}
