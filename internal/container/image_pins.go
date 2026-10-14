// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package container

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
)

// Image pins coordinate deploys with the superseded image cleanup. A deploy
// pins the generated image it is about to build or run from; the cleanup
// (removeAppImages) skips pinned images. The pin is taken together with the
// deploy's image existence check under imageSweepMu, and the cleanup checks
// the pins and removes an image under the same lock, so the cleanup can never
// remove an image between a deploy finding it present and starting a
// container from it: either the pin is seen, or the image is gone before the
// deploy looks and the deploy builds it. Pins are process wide, like the
// container host they protect.
//
// A pin is released once the image is protected otherwise: by the version
// row that records it, or by the container running from it. The cleanup
// computes the images to keep from the versions in the database, and that
// snapshot can predate a commit whose pin is then released before the sweep.
// Every release therefore advances the image generation; the cleanup reads
// the generation before the database and, under the sweep lock, removes
// nothing once it has moved (see ErrImagesChanged), recomputing instead.
var (
	imageSweepMu    sync.RWMutex
	imagePinsMu     sync.Mutex
	imagePins       = map[ImageName]int{}
	imageGeneration atomic.Uint64
)

// ErrImagesChanged is returned by an image sweep whose set of images to keep
// was computed before a pin was released (a deploy committed in between):
// the caller recomputes the set and sweeps again
var ErrImagesChanged = errors.New("images changed while the superseded images were computed")

// ImageGeneration returns the image generation, advanced whenever an image
// pin is released. A sweep computed against an older generation is stale.
// Generations start at 1: a zero generation passed to a sweep means no check
func ImageGeneration() uint64 {
	return imageGeneration.Load() + 1
}

// PinImage marks name as in use by a deploy until the returned release
// function is called (idempotent). Pinned images are not removed by the
// superseded image cleanup. Use PinImageAndCheck when the deploy decides
// whether to build from the image's presence.
func PinImage(name ImageName) (release func()) {
	if name == "" {
		return func() {}
	}
	imagePinsMu.Lock()
	imagePins[name]++
	imagePinsMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			imagePinsMu.Lock()
			defer imagePinsMu.Unlock()
			if imagePins[name] <= 1 {
				delete(imagePins, name)
			} else {
				imagePins[name]--
			}
			imageGeneration.Add(1)
		})
	}
}

// ImagePins holds the pins an operation takes on the images it deploys
// from, released together when the operation is over. A deploy transaction
// carries one (DeployTxn.RegisterImage); an operation without a deploy
// transaction, the lazy container start of a request, attaches one to its
// context (ContextWithImagePins) so the pin outlives the start and covers
// the recording of the image on the version
type ImagePins struct {
	mu       sync.Mutex
	names    []ImageName
	releases []func()
}

// Pin pins the image until Release (idempotent per image)
func (p *ImagePins) Pin(name ImageName) {
	if name == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, pinned := range p.names {
		if pinned == name {
			return
		}
	}
	p.names = append(p.names, name)
	p.releases = append(p.releases, PinImage(name))
}

// Names returns the pinned images
func (p *ImagePins) Names() []ImageName {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]ImageName(nil), p.names...)
}

// Release drops the pins. The names stay listed
func (p *ImagePins) Release() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, release := range p.releases {
		release()
	}
	p.releases = nil
}

type imagePinsKeyType struct{}

var imagePinsKey = imagePinsKeyType{}

// ContextWithImagePins attaches a new pin set to the context, for an
// operation without a deploy transaction. The caller releases it when the
// operation, including any metadata write recording the images, is over
func ContextWithImagePins(ctx context.Context) (context.Context, *ImagePins) {
	pins := &ImagePins{}
	return context.WithValue(ctx, imagePinsKey, pins), pins
}

// RegisterImage pins a generated image the operation in ctx is about to
// build or run from, on its deploy transaction when there is one, else on
// its pin set (ContextWithImagePins). No-op outside either
func RegisterImage(ctx context.Context, name ImageName) {
	if dt := DeployTxnFromContext(ctx); dt != nil {
		dt.RegisterImage(name)
		return
	}
	if pins, _ := ctx.Value(imagePinsKey).(*ImagePins); pins != nil {
		pins.Pin(name)
	}
}

// PinImageAndCheck pins the image and reports whether it exists, atomically
// with respect to the superseded image cleanup (see the pins comment). The
// pin is released on error.
func PinImageAndCheck(ctx context.Context, cm ContainerManager, name ImageName) (exists bool, release func(), err error) {
	imageSweepMu.RLock()
	defer imageSweepMu.RUnlock()
	release = PinImage(name)
	exists, err = cm.ImageExists(ctx, name)
	if err != nil {
		release()
		return false, nil, err
	}
	return exists, release, nil
}

// imagePinned reports whether the image, as listed by the container runtime
// (Podman adds a localhost/ repository prefix), is pinned
func imagePinned(listed string) bool {
	imagePinsMu.Lock()
	defer imagePinsMu.Unlock()
	for name := range imagePins {
		if listed == string(name) || strings.HasSuffix(listed, "/"+string(name)) {
			return true
		}
	}
	return false
}
