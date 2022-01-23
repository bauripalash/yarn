package internal

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIndent(t *testing.T) {
	assert := assert.New(t)

	t.Run("Empty", func(t *testing.T) {
		text := Indent("", "> ")
		assert.Equal("", text)
	})

	t.Run("MultiLine", func(t *testing.T) {
		text := Indent("foo\nbar\nbaz", "> ")
		assert.Equal("> foo\n> bar\n> baz", text)
	})
}
