//go:build !linux && !darwin

package main

import (
	"errors"

	"github.com/yongjohnlee80/gio/op/extimage"
)

const gpuProducerName = "gpu"

func newGPUProducer(extimage.Caps, bool) (producer, error) {
	return nil, errors.New("scene3d: no GPU producer on this platform")
}
