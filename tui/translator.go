package tui

// TRANSLATOR — the catalogs a [Message] is looked up in.
//
// The App asks one question of them: what an id says in a language. tui declares that question as
// the consumer, so the catalogs can be golib's i18n.Set (the default, i18n.Toolkit), a remote
// catalog, or a test's fake, and the runtime depends on none of them. The App asks again in
// i18n.English to keep a hotkey on English's letter (englishMnemonic). A catalog's fallback chain
// ("es_MX" → "es" → English) is the implementation's own.
//
// The App takes one through WithTranslator, SetTranslator and ApplyTranslator. WithTranslations,
// SetTranslations and ApplyTranslations are the same for an *i18n.Set, kept as they were.

// Translator gives the text id has in language ("ko_KR", "es"), and ok false when its catalogs have
// none. Called on the loop goroutine.
type Translator interface {
	Lookup(language, id string) (text string, ok bool)
}
