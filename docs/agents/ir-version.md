# IR version

Read before changing the shape of the IR or of DocContent.

`ir_version` 1 is frozen (docs/adr/0004-ir-and-links.md): extractions are stored as IR, and documents as DocContent, whose blocks are IR blocks (`internal/doccontent`, docs/adr/0006-editor-and-doc-content.md). Changing the IR's shape means a new version and, in the same change, a documented upgrade path for stored extractions and documents. Extractor changes that keep the shape need neither. Changing DocContent's shape means a new `doc_version` and a migration, tested up and down. The one exception is an optional field whose absence means what v1 meant before it, that changes no block and that readers can ignore, such as `code_langs`: it needs neither (ADR 0006).
