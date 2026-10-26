package embed

// cards are the measured cards (Catalog). Every number but Multilingual comes from a measurement its
// Source names: the download is the sum of the pulled layers, Dims and Context are /api/show's
// embedding_length and context_length, and each Resident sample is /api/ps's size with the model
// loaded at that num_ctx (which /api/ps reports back), unloaded between samples. Multilingual is
// each model's published card, not a measurement. The XLM-R encoders (bge-m3,
// snowflake-arctic-embed2) stop growing past 2048 tokens; the decoders (qwen3-embedding) keep
// growing with their KV cache.
var cards = []Card{
	{
		Provider: "ollama", Name: "qwen3-embedding:0.6b", Dims: 1024, Context: 32768, Download: 639150858,
		Resident:     []Sample{{2048, 994605793}, {8192, 1724414689}},
		Multilingual: true,
		Source:       measured("ac6da0dfba84a81fdbfbaf330198c33cd77c4cdfc53e8bc50eb581914a15621d"),
	},
	{
		Provider: "ollama", Name: "qwen3-embedding:4b", Dims: 2560, Context: 40960, Download: 2496704041,
		Resident:     []Sample{{2048, 3145769941}, {8192, 4076905429}},
		Multilingual: true,
		Source:       measured("df5bd2e3c74cd8d069d21dc038f1b359fcdc9458fce1c99bd43c9eb1518ff907"),
	},
	{
		Provider: "ollama", Name: "snowflake-arctic-embed2", Dims: 1024, Context: 8192, Download: 1160296718,
		Resident:     []Sample{{512, 1173346056}, {2048, 1244166879}, {8192, 1244166879}},
		Multilingual: true,
		Source:       measured("5de93a84837d0ff00da872e90830df5d973f616cbf1e5c198731ab19dd7b776b"),
	},
	{
		Provider: "ollama", Name: "bge-m3", Dims: 1024, Context: 8192, Download: 1157672605,
		Resident:     []Sample{{512, 1173346056}, {2048, 1244166879}, {8192, 1244166879}},
		Multilingual: true,
		Source:       measured("7907646426070047a77226ac3e684fbbe8410524f7b4a74d02837e43f2146bab"),
	},
	{
		Provider: "ollama", Name: "nomic-embed-text", Dims: 768, Context: 2048, Download: 274302450,
		Resident:     []Sample{{512, 300930825}, {2048, 389053152}},
		Multilingual: false,
		Source:       measured("0a109f422b47e3a30ba2b10eca18548e944e8a23073ee3f3e947efcf3c45e59f"),
	},
}

// measured is the Source of the 2026-10-26 measurements: Ollama 0.40.11
// (ollama/ollama@sha256:dea72d1e22270fe095b68fdf756d2b489d615643c5a2eaebbc5f569181e322d8) on a CPU,
// so a GPU fit is an estimate (GPUMargin).
func measured(digest string) Source {
	return Source{Digest: digest, Ollama: "0.40.11", Device: "cpu", Measured: "2026-10-26"}
}
