// Copyright 2017 orijtech. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package frontender_test

import (
	"io"
	"log"
	"os"

	"github.com/orijtech/frontender"
)

func Example_Listen() {
	lc, err := frontender.Listen(&frontender.Request{
		Domains: []string{
			"git.orijtech.com",
			"repo.orijtech.com",
		},
		NonHTTPSRedirectURL: "https://git.orijtech.com",

		ProxyAddresses: []string{
			"http://localhost:9845",
			"http://localhost:8888",
			"http://localhost:4447",
		},

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

func Example_GenerateBinary() {
	rc, err := frontender.GenerateBinary(&frontender.DeployInfo{
		FrontendConfig: &frontender.Request{
			Domains: []string{
				"www.medisa.orijtech.com",
				"medisa.orijtech.com",
				"m.orijtech.com",
			},
			ProxyAddresses: []string{
				"http://192.168.1.105:9855",
				"http://192.168.1.140:8998",
			},
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

func Example_GenerateDockerImage() {
	imageName, err := frontender.GenerateDockerImage(&frontender.DeployInfo{
		CanonicalImageNamePrefix: "frontender",
		FrontendConfig: &frontender.Request{
			Domains: []string{
				"git.orijtech.com",
				"repo.orijtech.com",
			},
			NonHTTPSRedirectURL: "https://git.orijtech.com",

			ProxyAddresses: []string{
				"http://localhost:9845",
				"http://192.168.1.100:9845",
			},

			NoAutoWWW: true,
		},
		ImageName: "odex-auto",
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("ImageName: %q\n", imageName)
}
