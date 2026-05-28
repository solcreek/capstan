package capstan

import (
	_ "embed"
)

//go:embed specs/hetzner.json
var hetznerSpecData []byte

//go:embed specs/digitalocean.json
var digitaloceanSpecData []byte

//go:embed specs/linode.json
var linodeSpecData []byte

//go:embed specs/vultr.json
var vultrSpecData []byte

//go:embed specs/posture.json
var postureSpecData []byte

var specs map[ProviderName]*ProviderSpec

func init() {
	specs = make(map[ProviderName]*ProviderSpec, 4)
	for name, data := range map[ProviderName][]byte{
		Hetzner:      hetznerSpecData,
		DigitalOcean: digitaloceanSpecData,
		Linode:       linodeSpecData,
		Vultr:        vultrSpecData,
	} {
		s, err := LoadSpec(data)
		if err != nil {
			panic("capstan: bad embedded spec for " + string(name) + ": " + err.Error())
		}
		specs[name] = s
	}
}

// Spec returns the embedded provider spec for the given provider.
func Spec(name ProviderName) *ProviderSpec {
	return specs[name]
}
