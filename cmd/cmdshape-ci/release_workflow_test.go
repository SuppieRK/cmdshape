package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

var _ = Describe("release distribution workflow", func() {
	var workflow string

	BeforeEach(func() {
		raw, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "release-distribution.yml"))
		Expect(err).NotTo(HaveOccurred())
		workflow = string(raw)
		var document map[string]any
		Expect(yaml.Unmarshal(raw, &document)).To(Succeed())
	})

	It("pins every third-party action to an immutable commit", func() {
		usesPattern := regexp.MustCompile(`(?m)^\s*uses:\s+[^@\s]+@([0-9a-f]{40})(?:\s+#.*)?$`)
		allUses := regexp.MustCompile(`(?m)^\s*uses:\s+.*$`).FindAllString(workflow, -1)
		pinnedUses := usesPattern.FindAllStringSubmatch(workflow, -1)

		Expect(pinnedUses).To(HaveLen(len(allUses)))
		Expect(workflow).NotTo(MatchRegexp(`uses:\s+[^@\s]+@v[0-9]`))
	})

	It("resolves one existing tag identity and checks out its exact source SHA", func() {
		for _, snippet := range []string{
			`^[0-9]+\.[0-9]+\.[0-9]+$`,
			`refs/cmdshape-release-tags/${TAG}^{commit}`,
			`source_sha=${SOURCE_SHA}`,
			`tag_oid=${TAG_OID}`,
			`ref: ${{ needs.preflight.outputs.source_sha }}`,
			`test "$(git rev-parse HEAD)" = "${EXPECTED_SHA}"`,
		} {
			Expect(workflow).To(ContainSubstring(snippet))
		}
		Expect(strings.Count(workflow, `test "$(git rev-parse "refs/cmdshape-release-tags/${TAG}^{commit}")" = "${EXPECTED_SHA}"`)).To(BeNumerically(">=", 3))
		Expect(workflow).To(ContainSubstring("  validate-source:"))
		Expect(workflow).To(ContainSubstring("os: [ubuntu-latest, macos-latest, windows-latest]"))
		Expect(workflow).To(ContainSubstring("needs: [preflight, validate-source]"))
		Expect(workflow).To(ContainSubstring("run: ./scripts/validate.sh"))
		Expect(workflow).To(ContainSubstring("cancel-in-progress: false"))
		Expect(workflow).To(ContainSubstring("DISPATCH_TAG: ${{ inputs.tag }}"))
		Expect(workflow).NotTo(ContainSubstring(`TAG="${{ github.event.inputs.tag }}`))
	})

	It("validates cmdshape-only artifacts before draft smoke and publication", func() {
		for _, snippet := range []string{
			`goos: [linux, darwin, windows]`,
			`goarch: [amd64, arm64]`,
			`TARGET_GOOS: ${{ matrix.goos }}`,
			`TARGET_GOARCH: ${{ matrix.goarch }}`,
			`CGO_ENABLED=0 GOOS="$TARGET_GOOS" GOARCH="$TARGET_GOARCH"`,
			`go build -trimpath -ldflags "$ldflags"`,
			`CGO_ENABLED=0 go build -trimpath -ldflags "$ldflags" -o "$host_probe"`,
			`asset="cmdshape_${TAG}_${TARGET_GOOS}_${TARGET_GOARCH}.zip"`,
			`test "$(unzip -Z1 "$asset")" = "$cmdshape_bin"`,
			`source_sha:$source_sha`,
			`name '*.zip' | wc -l)" -eq 6`,
			`expected_binary="cmdshape"`,
			`expected_binary="cmdshape.exe"`,
			`test "$(jq -r .binary "$metadata_file")" = "$expected_binary"`,
			`test "$(unzip -Z1 "release/${asset}")" = "$expected_binary"`,
			`sha256sum ./cmdshape_*.zip > ./cmdshape_checksums.txt`,
			`test "$(wc -l < ./cmdshape_checksums.txt)" -eq 6`,
			`predicate-type: https://github.com/SuppieRK/cmdshape/attestations/binary-archive/v1`,
			`--pattern cmdshape_checksums.txt`,
			`./release/*_checksums.txt`,
			`draft: true`,
			`gh release download "$TAG"`,
			`gh release edit "$TAG" --repo "$REPO" --draft=false --latest`,
		} {
			Expect(workflow).To(ContainSubstring(snippet))
		}
		for _, forbidden := range []string{
			"\n          GOOS: ${{ matrix.goos }}",
			"\n          GOARCH: ${{ matrix.goarch }}",
		} {
			Expect(workflow).NotTo(ContainSubstring(forbidden))
		}

		draft := strings.Index(workflow, "  create-draft:")
		smoke := strings.Index(workflow, "  smoke-draft:")
		publish := strings.Index(workflow, "  publish:")
		Expect(draft).To(BeNumerically(">", 0))
		Expect(smoke).To(BeNumerically(">", draft))
		Expect(publish).To(BeNumerically(">", smoke))
		Expect(workflow[smoke:publish]).To(ContainSubstring("permissions:\n      contents: write"))
	})
})

var _ = Describe("workflow security contracts", func() {
	entries, err := filepath.Glob(filepath.Join("..", "..", ".github", "workflows", "*.yml"))
	Expect(err).NotTo(HaveOccurred())
	for _, path := range entries {
		It("pins external actions and parses "+filepath.Base(path), func() {
			raw, err := os.ReadFile(path)
			Expect(err).NotTo(HaveOccurred())
			var document map[string]any
			Expect(yaml.Unmarshal(raw, &document)).To(Succeed())
			workflow := string(raw)
			uses := regexp.MustCompile(`(?m)^\s*uses:\s+.*$`).FindAllString(workflow, -1)
			pinned := regexp.MustCompile(`(?m)^\s*uses:\s+[^@\s]+@[0-9a-f]{40}(?:\s+#.*)?$`).FindAllString(workflow, -1)
			Expect(pinned).To(HaveLen(len(uses)))
			Expect(workflow).To(ContainSubstring("permissions:\n  contents: read"))
		})
	}
})

var _ = Describe("validation workflow dependencies", func() {
	DescribeTable("allows the change planner to classify embedded Markdown",
		func(name string) {
			raw, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", name))
			Expect(err).NotTo(HaveOccurred())

			workflow := string(raw)
			Expect(workflow).NotTo(ContainSubstring("paths-ignore:\n      - '**/*.md'"))
		},
		Entry("main validation", "main-validation.yml"),
		Entry("pull-request validation", "pr-validation.yml"),
	)

	DescribeTable("using the Go module lock for validation tools",
		func(name string) {
			raw, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", name))
			Expect(err).NotTo(HaveOccurred())
			workflow := string(raw)
			var document map[string]any
			Expect(yaml.Unmarshal(raw, &document)).To(Succeed())

			Expect(workflow).To(ContainSubstring("go mod download"))
			Expect(workflow).NotTo(ContainSubstring("go install "))
			Expect(workflow).NotTo(ContainSubstring("raw.githubusercontent.com/golangci"))
			Expect(workflow).To(ContainSubstring(
				"uses: SonarSource/sonarqube-scan-action@ba9859eae8dd6bd29e412f25ddbbef3d032000f4 # v8",
			))
			Expect(workflow).NotTo(ContainSubstring("uses: SonarSource/sonarqube-scan-action@v8"))
		},
		Entry("main validation", "main-validation.yml"),
		Entry("pull-request validation", "pr-validation.yml"),
	)

	It("declares every executed validation tool in go.mod", func() {
		raw, err := os.ReadFile(filepath.Join("..", "..", "go.mod"))
		Expect(err).NotTo(HaveOccurred())
		module := string(raw)

		for _, tool := range []string{
			"github.com/fzipp/gocyclo/cmd/gocyclo",
			"github.com/golangci/golangci-lint/v2/cmd/golangci-lint",
			"github.com/gordonklaus/ineffassign",
			"golang.org/x/vuln/cmd/govulncheck",
			"honnef.co/go/tools/cmd/staticcheck",
		} {
			Expect(module).To(ContainSubstring("\n\t" + tool + "\n"))
		}
	})
})
