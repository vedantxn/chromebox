package vm

import (
	"fmt"
	"regexp"
)

// Image describes a golden image: how large a VM to give it, and what the
// guest prints on its serial console when the app inside is up. The host
// watches the log for that line instead of guessing with a sleep.
type Image struct {
	VCPUs  int
	MemMiB int
	Ready  *regexp.Regexp // first capture group, if any, is the interesting part
}

var images = map[string]Image{
	"counter": {VCPUs: 1, MemMiB: 256, Ready: countLine},
	"chrome":  {VCPUs: 2, MemMiB: 1024, Ready: regexp.MustCompile(`(?m)^chrome ready (.*)\r?\n`)},
}

// LookupImage returns the settings for a golden image by name.
func LookupImage(name string) (Image, error) {
	img, ok := images[name]
	if !ok {
		return Image{}, fmt.Errorf("unknown image %q", name)
	}
	return img, nil
}
