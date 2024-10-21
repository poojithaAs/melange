//go:build integration
// +build integration

package build

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"io"

	"chainguard.dev/melange/pkg/container"
	"chainguard.dev/melange/pkg/container/docker"
	"github.com/google/go-cmp/cmp"
)

func TestBuild_BuildPackage(t *testing.T) {
	tests := []struct {
		name            string
		expectedVersion string
	}{
		{
			name:            "crane",
			expectedVersion: "0.20.2-r1",
		},
	}

	const arch = "x86_64"

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			p := filepath.Join("testdata", "build_configs", tt.name) + ".yaml"

			t.Run("builds successfully", func(t *testing.T) {
				ctx := context.Background()

				// NOTE: Ideally we have one runner that works everywhere to make it easier to
				// work on these tests. But until then, we'll try to use the most appropriate
				// runner for the environment.
				r := getRunner(ctx, t)

				b, err := New(
					ctx,
					WithConfig(p),
					WithOutDir(tempDir),
					WithArch(arch),
					WithConfigFileRepositoryURL("https://github.com/wolfi-dev/os"),
					WithConfigFileRepositoryCommit("c0ffee"),
					WithRunner(r),
					WithNamespace("wolfi"),
					WithExtraRepos([]string{"https://packages.wolfi.dev/os"}),
					WithExtraKeys([]string{"https://packages.wolfi.dev/os/wolfi-signing.rsa.pub"}),
				)
				if err != nil {
					t.Fatalf("setting up build: %v", err)
				}

				if err := b.BuildPackage(ctx); err != nil {
					t.Fatalf("building package: %v", err)
				}

				t.Run("sbom correctness", func(t *testing.T) {
					apkPath := filepath.Join(tempDir, arch, fmt.Sprintf("%s-%s.apk", tt.name, tt.expectedVersion))
					apkFile, err := os.Open(apkPath)
					if err != nil {
						t.Fatalf("opening apk: %v", err)
					}
					defer apkFile.Close()

					gr, err := gzip.NewReader(apkFile)
					if err != nil {
						t.Fatalf("creating gzip reader: %v", err)
					}
					defer gr.Close()

					tr := tar.NewReader(gr)
					var sbom io.Reader
					sbomPath := fmt.Sprintf("var/lib/db/sbom/%s-%s.spdx.json", tt.name, tt.expectedVersion)
					for {
						hdr, err := tr.Next()
						if err != nil {
							t.Fatalf("reading tar header: %v", err)
						}
						if hdr.Name == sbomPath {
							sbom = tr
							break
						}
					}
					if sbom == nil {
						t.Fatalf("SBOM not found in apk: %s", sbomPath)
					}

					expectedSBOMPath := filepath.Join("testdata", "goldenfiles", "sboms", fmt.Sprintf("%s-%s.spdx.json", tt.name, tt.expectedVersion))
					expectedSbomFile, err := os.Open(expectedSBOMPath)
					if err != nil {
						t.Fatalf("opening expected SBOM: %v", err)
					}

					expected, err := io.ReadAll(expectedSbomFile)
					if err != nil {
						t.Fatalf("reading expected SBOM: %v", err)
					}
					actual, err := io.ReadAll(sbom)
					if err != nil {
						t.Fatalf("reading actual SBOM: %v", err)
					}

					if diff := cmp.Diff(expected, actual); diff != "" {
						t.Fatalf("SBOMs differ: \n%s\n", diff)
					}
				})
			})
		})
	}
}

func getRunner(ctx context.Context, t *testing.T) container.Runner {
	t.Helper()

	if r := container.BubblewrapRunner(true); r.TestUsability(ctx) {
		return r
	}

	r, err := docker.NewRunner(ctx)
	if err != nil {
		t.Fatalf("creating docker runner: %v", err)
	}
	if r.TestUsability(ctx) {
		return r
	}

	t.Fatal("no usable runner found")
	return nil
}
