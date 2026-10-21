package vm

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Image describes a golden image: how large a VM to give it, and what the
// guest prints on its serial console when the app inside is up. The host
// watches the log for that line instead of guessing with a sleep.
type Image struct {
	VCPUs  int
	MemMiB int
	Ready  *regexp.Regexp // first capture group, if any, is the interesting part
	// CDPPort is the TCP port, on the guest's own address, where an agent
	// reaches Chrome's DevTools protocol. 0 for images without a browser.
	CDPPort int
	// Counter is true for the counter guest, whose count lines let the host
	// check that a restored VM continued rather than rebooted.
	Counter bool
}

var images = map[string]Image{
	"counter": {VCPUs: 1, MemMiB: 256, Ready: countLine, Counter: true},
	"chrome":  {VCPUs: 2, MemMiB: 1024, Ready: regexp.MustCompile(`(?m)^chrome ready (.*)\r?\n`), CDPPort: 9223},
}

// LookupImage returns the settings for a golden image by name.
func LookupImage(name string) (Image, error) {
	img, ok := images[name]
	if !ok {
		return Image{}, fmt.Errorf("unknown image %q", name)
	}
	return img, nil
}

// ImageOf returns the name and settings of the golden image a VM was made
// from, read from its directory or, on a node that only holds a received
// snapshot, from the snapshot.
func ImageOf(p Paths) (string, Image, error) {
	for _, f := range []string{p.ImageFile(), p.SnapImage()} {
		b, err := os.ReadFile(f)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", Image{}, err
		}
		name := strings.TrimSpace(string(b))
		img, err := LookupImage(name)
		return name, img, err
	}
	return "", Image{}, fmt.Errorf("no image recorded for %s", p.Dir)
}
