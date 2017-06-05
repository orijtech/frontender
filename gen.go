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

package frontender

import (
	"bytes"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/odeke-em/go-uuid"
)

var errUnimplemented = errors.New("unimplemented")

// Goal: Generate the binary so that it can be deployed as a disk image or a Dockerfile.

type DeployInfo struct {
	FrontendConfig *Request
	SourceImage    string
	ImageName      string

	TargetGOOS string
	Environ    []string

	CanonicalImageName       string `json:"canonical_image_name"`
	CanonicalImageNamePrefix string `json:"canonical_image_name_prefix"`
}

func GenerateDockerImageForGCE(req *DeployInfo) (imageName string, err error) {
	return "", errUnimplemented
}

type BinaryHandle struct {
	done      func() error
	rc        io.ReadCloser
	path      string
	binDir    string
	closeOnce sync.Once
}

func (bh *BinaryHandle) Read(b []byte) (int, error) {
	return bh.rc.Read(b)
}

func (bh *BinaryHandle) Close() error {
	var err error = io.EOF
	bh.closeOnce.Do(func() {
		err = bh.rc.Close()
		if bh.done != nil {
			e := bh.done()
			if err == nil {
				err = e
			}
		}
	})
	return err
}

var _ io.ReadCloser = (*BinaryHandle)(nil)

func GenerateBinary(req *DeployInfo) (io.ReadCloser, error) {
	return generateBinary(req)
}

func generateBinary(req *DeployInfo) (*BinaryHandle, error) {
	// 1. Generate the main.go file:
	binDir := fmt.Sprintf("./bin/%s", uuid.NewRandom())
	if err := os.MkdirAll(binDir, 0777); err != nil {
		return nil, err
	}
	abort := func() error { return os.RemoveAll(binDir) }

	goMainFilepath := filepath.Join(binDir, "main.go")
	f, err := os.Create(goMainFilepath)
	if err != nil {
		abort()
		return nil, err
	}

	if err := req.FrontendConfig.Validate(); err != nil {
		abort()
		return nil, err
	}

	err = mainTmpl.Execute(f, req.FrontendConfig)
	_ = f.Close()

	if err != nil {
		abort()
		return nil, err
	}

	// 2. Next step is to build the binary
	binaryPath := filepath.Join(binDir, "generated-exec")
	cmdArgs := []string{"build", "-o", binaryPath, binDir}
	cmd := exec.Command("go", cmdArgs...)

	// 2.1. Build the environment for the comment
	cmd.Env = append(cmd.Env, os.Environ()...)
	if goos := strings.TrimSpace(req.TargetGOOS); goos != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("GOOS=%s", goos))
	}
	if len(req.Environ) > 0 {
		cmd.Env = append(cmd.Env, req.Environ...)
	}

	if resp, err := cmd.CombinedOutput(); err != nil {
		if len(bytes.TrimSpace(resp)) > 0 {
			err = errors.New(string(resp))
		}
		abort()
		return nil, err
	}
	f, err = os.Open(binaryPath)
	if err != nil {
		abort()
		return nil, err
	}

	bh := &BinaryHandle{
		rc:     f,
		path:   binaryPath,
		done:   abort,
		binDir: binDir,
	}
	return bh, nil
}

func GenerateDockerImage(req *DeployInfo) (imageName string, err error) {
	// 1. Generate the binary
	bh, err := generateBinary(req)
	if err != nil {
		return "", err
	}
	defer bh.Close()

	binDir := bh.binDir
	binaryPath := bh.path

	// 2. Generate the Dockerfile.
	dockerFilePath := filepath.Join(binDir, "Dockerfile")
	dockerFile, err := os.Create(dockerFilePath)
	if err != nil {
		return "", err
	}

	binaryBasePath := filepath.Base(binaryPath)
	dockerConfig := &DockerConfig{
		BinaryPath:  binaryBasePath,
		SourceImage: req.SourceImage,
		ImageName:   imageNameOrGenerated(req.ImageName),
	}
	err = dockerFileTmpl.Execute(dockerFile, dockerConfig)
	_ = dockerFile.Close()
	if err != nil {
		return "", err
	}

	canonicalImageName := ensureCanonicalImage(req)
	dockerBuildArgs := []string{"build", "-t", canonicalImageName, binDir}
	cmd := exec.Command("docker", dockerBuildArgs...)
	if resp, err := cmd.CombinedOutput(); err != nil {
		if len(bytes.TrimSpace(resp)) > 0 {
			err = errors.New(string(resp))
		}
		return "", err
	}

	return canonicalImageName, nil
}

func ensureCanonicalImage(req *DeployInfo) string {
	if name := req.CanonicalImageName; name != "" {
		return name
	}
	suffix := fmt.Sprintf("generated-%d", time.Now().Unix())
	if req.CanonicalImageNamePrefix == "" {
		return suffix
	}
	return req.CanonicalImageNamePrefix + "/" + suffix
}

type Dependency struct {
	LocalPath  string
	DockerPath string
}

type DockerConfig struct {
	PrerunCommands []string      `json:"prerun_commands"`
	Dependencies   []*Dependency `json:"dependencies"`
	ImageName      string        `json:"image_name"`
	SourceImage    string        `json:"source_image"`
	BinaryPath     string        `json:"binary_path"`
}

const dockerFileBody = `
from {{imageOrDefault .SourceImage}}

ADD {{.BinaryPath}} {{.ImageName}}

{{range .Dependencies}}
ADD {{.LocalPath}} {{.DockerPath}}
{{end}}
{{if .PrerunCommands}}{{range .PrerunCommands}}CMD ["{{.}}"]{{end}}{{end}}

CMD ["./{{.ImageName}}"]
`

func imageNameOrGenerated(img string) string {
	if img != "" {
		return img
	}
	return fmt.Sprintf("%s-generated", uuid.NewRandom())
}

var funcs = template.FuncMap{
	"gobEncodeAndQuote": func(v interface{}) string {
		buf := new(bytes.Buffer)
		_ = gob.NewEncoder(buf).Encode(v)
		return strconv.Quote(string(buf.Bytes()))
	},

	"imageNameOrGenerated": imageNameOrGenerated,

	"imageOrDefault": func(imageName string) string {
		if imageName == "" {
			return "debian:jessie"
		} else {
			return imageName
		}
	},
}

var (
	mainTmpl       = template.Must(template.New("mainTmpl").Funcs(funcs).Parse(mainBody))
	dockerFileTmpl = template.Must(template.New("dockerfile").Funcs(funcs).Parse(dockerFileBody))
)

const mainBody = `
package main

import (
	"log"
	"encoding/gob"
	"strings"

	"github.com/orijtech/frontender"
)

func main() {
	buf := strings.NewReader({{gobEncodeAndQuote .}})
	req := new(frontender.Request)
	if err := gob.NewDecoder(buf).Decode(req); err != nil {
		log.Fatalf("gobDecoding err: %v", err)
	}
	lc, err := frontender.Listen(req)
	if err != nil {
		log.Fatal(err)
	}
	defer lc.Close()

	if err := lc.Wait(); err != nil {
		log.Fatal(err)
	}
}
`
