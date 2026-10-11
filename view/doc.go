// Package view materializes documents from a database with a declarative .view file: a query that
// returns one JSON value per row, and a template that renders each row into a document in any text
// format. It separates shaping data for a consumer, such as an embedding model, a search index or an
// export, from the consumer itself.
//
// # Architectural Model
//
// A .view file pairs a structured database query with a templated document layout:
//
//	---
//	version: 1
//	name: track_view
//	source: postgres
//	args: [entity_ids]
//	process: |
//	  SELECT jsonb_build_object('entity_id', t.id, 'track', t.title, 'lyric', t.lyric)
//	  FROM track t WHERE t.id = ANY($1)
//	export: track_{{.entity_id}}.md
//	---
//	# {{.track}}
//	{{if .lyric}}{{.lyric}}{{else}}*(Instrumental)*{{end}}
//
// golib/parse/view reads and validates the file's syntax; this package evaluates it against a live
// database connection. Each row's JSON result can be consumed in three primary ways:
// decoded into the caller's own Go struct, rendered directly to a string, or streamed via [io.Reader]:
//
//	v, err := view.Load("track.view")
//	if err != nil {
//		return err
//	}
//	rows, err := v.WithArgs(ids).Query(ctx, conn) // conn is a dao.DataConn
//	if err != nil {
//		return err
//	}
//	defer rows.Close()
//	for x, err := range rows.All() {
//		if err != nil {
//			return err
//		}
//		err = json.Unmarshal(x, &track)  // 1. Decode raw row payload
//		doc, err := v.Parse(x)           // 2. Render document to string
//		rdr, err := v.Read(x)            // 3. Stream document via io.ReadCloser
//		defer rdr.Close()
//	}
//
// [Invocation.Export] writes every row's document to a [github.com/yongjohnlee80/golib/vfs.FS] at
// the path rendered by the export template for the declared destination.
//
// # Safety and Invariants
//
//   - SQL Injection Immunity: The query declared in process is never treated as a template.
//     Arguments are always passed to the driver as bound parameters ($1, $2, …).
//     Any occurrence of "{{" inside the process field is rejected at compilation time.
//   - Context-Aware Escaping: Every value printed by the template body is escaped according to the
//     declared output format:
//   - text, markdown: printed verbatim without escaping.
//   - json: escaped for inclusion inside a JSON string's quotes (quotes, backslashes, control characters).
//   - xml: escaped as XML character data (&, <, >, ', ").
//   - html: escaped by [html/template] according to the exact context of the action (HTML body text,
//     element attribute, URL, script, or style). Active script schemes (such as "javascript:")
//     are sanitized.
//   - Trusted Output Exceptions: In text, markdown, json, and xml formats, values produced by "toJson",
//     "quote", or "raw" return [Raw] and bypass the format's escaping. In html format, however, "toJson"
//     and "quote" are treated as regular text and contextually escaped by [html/template]; only "raw"
//     marks trusted HTML markup (returning [html/template.HTML]); contextual processing still applies
//     in attributes, URLs, scripts, and styles.
//   - Null Safety: NULL or missing database fields print as empty strings rather than failing.
//   - Path Traversal Containment: Export filenames that attempt directory traversal ("../") or
//     contain absolute paths are rejected by the target filesystem driver.
package view
