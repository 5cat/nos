package mig

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestProfileName__getMemorySlices(t *testing.T) {
	assert.Equal(t, 20, profile3g20gb.getMemorySlices())
}

func TestProfileName__getGiSlices(t *testing.T) {
	assert.Equal(t, 3, profile3g20gb.getGiSlices())
}
