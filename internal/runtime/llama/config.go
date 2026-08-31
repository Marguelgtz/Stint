package llama

const DefaultInteractivePort = 8409
const DefaultDeepAPort = 8301
const DefaultDeepBPort = 8302

type Config struct {
	Model   string
	Context int
	Port    int
}

func InteractiveQwen() Config {
	return Config{Model: "qwen3.8-27b", Context: 32768, Port: DefaultInteractivePort}
}
