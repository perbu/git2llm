package tokens

import (
	"cloud.google.com/go/vertexai/genai"
	genaitok "cloud.google.com/go/vertexai/genai/tokenizer"

	"fmt"
	"github.com/tiktoken-go/tokenizer"
	"strings"
)

type Counter struct {
	encoding  tokenizer.Codec
	model     string
	gencoding *genaitok.Tokenizer
}

func New(model string) (*Counter, error) {
	if strings.HasPrefix(model, "gemini") {
		genc, err := genaitok.New(model)
		if err != nil {
			return nil, fmt.Errorf("vertexai/genai/tokenizer.New: %w", err)
		}
		return &Counter{
			gencoding: genc,
			model:     model,
		}, nil
	}

	enc, err := tokenizer.Get(tokenizer.Encoding(model))
	if err != nil {
		return nil, fmt.Errorf("tokenizer.Get: %w", err)
	}
	return &Counter{
		encoding: enc,
		model:    model,
	}, nil
}

func (c Counter) Count(text string) (int, error) {
	if c.gencoding != nil {
		resp, err := c.gencoding.CountTokens(genai.Text(text))
		if err != nil {
			return 0, fmt.Errorf("vertexai/genai/tokenizer.CountTokens: %w", err)
		}
		return int(resp.TotalTokens), nil
	}
	return c.encoding.Count(text)
}

func (c Counter) Model() string {
	return c.model
}
