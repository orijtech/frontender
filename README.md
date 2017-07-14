# frontender
![](./assets/logo.png)

Setup a server frontend with HTTPS that then proxies to traffic to a backend/cluster.

This project is used inside orijtech to create for folks HTTPS servers that can then be
put in Docker images, or automatically uploaded to respective cloud storage systems
and passed into some container engine for a disk image.

## Examples:
* Preamble with imports:
```go
package frontender_test

import (
	"io"
	"log"
	"os"

	"github.com/orijtech/frontender"
)
```

* Plain listen as a server:
```go
func listen() {
	lc, err := frontender.Listen(&frontender.Request{
		Domains: []string{
			"git.orijtech.com",
			"repo.orijtech.com",
		},
		NonHTTPSRedirectURL: "https://git.orijtech.com",

		ProxyAddress: "http://localhost:9845",

		NoAutoWWW: true,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer lc.Close()

	if err := lc.Wait(); err != nil {
		log.Fatal(err)
	}
}
```

* Generate the binary of the server for a platform
```go
func generateBinary() {
	rc, err := frontender.GenerateBinary(&frontender.DeployInfo{
		FrontendConfig: &frontender.Request{
			Domains: []string{
				"www.medisa.orijtech.com",
				"medisa.orijtech.com",
				"m.orijtech.com",
			},
			ProxyAddress: "http://192.168.1.105:9855",
		},
		TargetGOOS: "linux",
		Environ:    []string{"CGO_ENABLED=0"},
	})
	if err != nil {
		log.Fatal(err)
	}
	defer rc.Close()

	f, err := os.Create("theBinary")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	io.Copy(f, rc)
}
```

* Generate a Docker image for the server
```go
func generateDockerImage() {
	imageName, err := frontender.GenerateDockerImage(&frontender.DeployInfo{
		CanonicalImageNamePrefix: "frontender",
		FrontendConfig: &frontender.Request{
			Domains: []string{
				"git.orijtech.com",
				"repo.orijtech.com",
			},
			NonHTTPSRedirectURL: "https://git.orijtech.com",

			ProxyAddress: "http://localhost:9845",

			NoAutoWWW: true,
		},
		ImageName: "odex-auto",
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("ImageName: %q\n", imageName)
}
```
