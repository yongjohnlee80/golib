// Package view materializes documents from a database with a declarative .view file: a query that
// returns one JSON value per row, and a template that renders each row into a document in any text
// format. It separates shaping data for a consumer, such as an embedding model, a search index or an
// export, from the consumer itself.
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
// golib/parse/view reads the file; this package evaluates it. Each row's JSON can be consumed three
// ways: decoded into the caller's own struct, rendered to a string, or streamed:
//
//	v, err := view.Load("track.view")
//	rows, err := v.WithArgs(ids).Query(ctx, conn) // conn is a dao.DataConn
//	defer rows.Close()
//	for x, err := range rows.All() {
//		err = json.Unmarshal(x, &track)  // the row itself
//		doc, err := v.Parse(x)           // the document, as a string
//		rdr, err := v.Read(x)            // the document, as a stream; Close it
//	}
//
// [Invocation.Export] writes every row's document to a vfs.FS at the name the export template
// renders, for the declared destination.
//
// # Safety
//
// The query is never a template. Arguments are bound parameters, so a value from a search hit or a
// request cannot change the SQL. Every value the body prints is escaped for the view's output
// format: JSON string content for json, character data for xml and html. A lyric holding a quote
// cannot break a JSON document. A NULL or missing value prints as nothing. An export name that would
// leave the destination is refused by the filesystem.
package view
