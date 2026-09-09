package lifecycle

import (
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("installer bootstrap", func() {
	DescribeTable("never executing a failed partial script download",
		func(source, client string) {
			workspace := GinkgoT().TempDir()
			bin := filepath.Join(workspace, "bin")
			marker := filepath.Join(workspace, "executed")
			downloads := filepath.Join(workspace, "downloads")
			Expect(os.MkdirAll(bin, 0o755)).To(Succeed())
			Expect(os.MkdirAll(downloads, 0o755)).To(Succeed())
			partial := "printf executed > " + shellQuoteTestPath(marker) + "\n"
			writeExecutable(filepath.Join(bin, client), `#!/bin/sh
out=""
while [ "$#" -gt 0 ]; do
  case "$1" in -o|-O) out="$2"; shift 2 ;; *) shift ;; esac
done
if [ -n "$out" ]; then
  printf %s `+shellQuoteArg(partial)+` > "$out"
else
  printf %s `+shellQuoteArg(partial)+`
fi
echo 'curl: (18) transfer closed with bytes remaining' >&2
exit 18
`)
			result := runInstallScriptSnippet("", installerBootstrapCommand(source, client), map[string]string{
				"PATH": testPATH(bin, os.Getenv("PATH")), "TMPDIR": testShellPath(downloads),
			})
			Expect(result.exitCode).NotTo(BeZero())
			Expect(result.stderr).To(ContainSubstring("installer bootstrap: " + client + " download failed"))
			Expect(marker).NotTo(BeAnExistingFile())
			entries, err := os.ReadDir(downloads)
			Expect(err).NotTo(HaveOccurred())
			Expect(entries).To(BeEmpty())
		},
		Entry("README curl command", "readme", "curl"),
		Entry("website curl command", "site", "curl"),
		Entry("README wget command", "readme", "wget"),
		Entry("website wget command", "site", "wget"),
	)

	DescribeTable("installing from a completely downloaded script",
		func(source, client string) {
			fixture := newInstallerDownloadFixture()
			response := `case "$url" in
  https://raw.githubusercontent.com/SuppieRK/cmdshape/main/scripts/install.sh)
    cp ` + shellQuoteTestPath(fixture.script) + ` "$out"; exit 0 ;;
esac
`
			if client == "curl" {
				fixture.curlResponse(response)
			} else {
				fixture.isolateTools()
				fixture.wgetResponse(response)
			}
			result := runInstallScriptSnippet("", installerBootstrapCommand(source, client), map[string]string{
				"PATH": fixture.path, "HOME": fixture.home, "SHELL": "/bin/sh",
				"VERSION": "1.2.3", "CMDSHAPE_INSTALL_DIR": fixture.install,
			})
			Expect(result.exitCode).To(BeZero(), result.stderr)
			Expect(fixture.installedVersion()).To(Equal("1.2.3\n"))
		},
		Entry("README curl command", "readme", "curl"),
		Entry("website curl command", "site", "curl"),
		Entry("README wget command", "readme", "wget"),
		Entry("website wget command", "site", "wget"),
	)
})

func installerBootstrapCommand(source, client string) string {
	if source == "readme" {
		body := string(mustReadFile(filepath.Join("..", "..", "README.md")))
		heading := "Install the latest release:"
		if client == "wget" {
			heading = "Or use wget:"
		}
		_, section, found := strings.Cut(body, heading)
		Expect(found).To(BeTrue())
		_, block, found := strings.Cut(section, "```bash\n")
		Expect(found).To(BeTrue())
		command, _, found := strings.Cut(block, "```")
		Expect(found).To(BeTrue())
		return command
	}
	body := string(mustReadFile(filepath.Join("..", "..", "site", "index.html")))
	id := "install-command"
	if client == "wget" {
		id += "-wget"
	}
	match := regexp.MustCompile(`(?s)<pre id="` + id + `"><code>(.*?)</code></pre>`).FindStringSubmatch(body)
	Expect(match).To(HaveLen(2))
	return html.UnescapeString(match[1])
}
