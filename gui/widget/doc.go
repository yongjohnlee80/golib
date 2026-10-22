// Package widget holds gui's native widgets: components that lay out in tui's cells and draw
// natively when a gui backend hosts them, and in cells on any other backend.
//
// # Capabilities
//
// What a host supplies, or what the widgets read of a value they are given, through small
// interfaces:
//
//	Capability          Method(s)                          Purpose
//	Diagrammer          Diagram                            draws a diagram block as a picture; Diagrams is golib's
//	DiagramLanguages    DiagramLanguage                    names the languages a Diagrammer draws: an HTMLView's <pre> classes
//	DiagramEngine       Langs, Lay, Declines               one language Diagrams lays out natively, beside Mermaid and Vega-Lite
//	LaidPicture         Paint, Size                        a laid-out diagram or chart, as a DiagramEngine returns it
//	DiagramHost         Diagram                            asks a Diagrammer for a block's picture on a widget's behalf
//	Renderer            Blocks, LayOut                     lays out the Editor's lines for the Rendered view
//	ReflowRenderer      ReflowFrom                         a Renderer whose later source can change earlier blocks
//	StructureRenderer   InCode, InTable                    a Renderer that knows its code and tables, for list editing
//	DocumentView        ShowDocument, SetVisible           a Rendered view drawn as one widget: a page or a CSV table
package widget
