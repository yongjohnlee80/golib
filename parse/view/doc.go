// Package view parses a .view file into its parts and resolves nothing: the frontmatter is a YAML
// tree with no field checked, and the body is a text/template tree with no function name checked.
// Deciding what a view means, and running it, is golib/view's job.
//
//	---
//	version: 1
//	name: track_view
//	source: postgres
//	args: [entity_ids]
//	process: |
//	  SELECT jsonb_build_object('entity_id', t.id, 'track', t.title)
//	  FROM track t WHERE t.id = ANY($1)
//	---
//	# {{.track}}
//
// The first line holds exactly "---", the frontmatter runs to the next line holding exactly "---",
// and the body is everything after that line. The body may begin with "---" of its own, a Markdown
// document's frontmatter; only the first pair of delimiter lines is the view's.
//
//	f, err := view.Parse(src, view.WithName("track.view")) // an ill-formed file is an *Error
//	_ = f.Meta                                             // the frontmatter's YAML document
//	_ = f.Templates[view.BodyTemplate]                     // the body's template tree
//
// Every position is the file's own. A YAML error inside the frontmatter is reported at its line in
// the file, and the body is parsed so that template line numbers count from the top of the file too.
package view
