package lifecycle

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("installer downloads", func() {
	It("installs latest through the public release redirect when the API is unavailable", func() {
		fixture := newInstallerDownloadFixture()
		fixture.curlResponse(`case "$url" in
  https://github.com/SuppieRK/cmdshape/releases/latest)
    printf 'HTTP/2 302\r\nLocation: https://github.com/SuppieRK/cmdshape/releases/tag/1.2.3\r\n\r\n' > "$headers"
    exit 0 ;;
  https://api.github.com/*) echo 'API rate limit exceeded' >&2; exit 22 ;;
esac
`)

		result := fixture.run("latest")

		Expect(result.exitCode).To(BeZero(), result.stderr)
		Expect(result.stdout).To(ContainSubstring("Installed binary cmdshape 1.2.3"))
		Expect(fixture.installedVersion()).To(Equal("1.2.3\n"))
	})

	It("recovers a curl 403 with wget and installs a verified archive", func() {
		fixture := newInstallerDownloadFixture()
		fixture.curlResponse("printf 'HTTP/2 403\\r\\n\\r\\n' > \"$headers\"\necho 'curl: (22) HTTP 403' >&2\nexit 22\n")
		fixture.wgetResponse("")

		result := fixture.run("1.2.3")

		Expect(result.exitCode).To(BeZero(), result.stderr)
		Expect(fixture.installedVersion()).To(Equal("1.2.3\n"))
	})

	It("installs latest with wget when curl is not installed", func() {
		fixture := newInstallerDownloadFixture()
		fixture.isolateTools()
		fixture.wgetResponse(`case "$url" in
  https://github.com/SuppieRK/cmdshape/releases/latest)
    printf '  HTTP/1.1 302 Found\r\n  Location: https://github.com/SuppieRK/cmdshape/releases/tag/1.2.3\r\n\r\n' >&2
    exit 8 ;;
esac
`)
		result := fixture.run("latest")
		Expect(result.exitCode).To(BeZero(), result.stderr)
		Expect(fixture.installedVersion()).To(Equal("1.2.3\n"))
	})

	It("retries an interrupted successful HTTP response without retaining partial bytes", func() {
		fixture := newInstallerDownloadFixture()
		fixture.curlResponse(`case "$url" in
  */cmdshape_1.2.3_linux_amd64.zip)
    if [ ! -f ` + shellQuoteTestPath(filepath.Join(fixture.root, "interrupted")) + ` ]; then
      printf started > ` + shellQuoteTestPath(filepath.Join(fixture.root, "interrupted")) + `
      printf partial > "$out"
      printf 'HTTP/2 200\r\n\r\n' > "$headers"
      echo 'curl: (18) transfer closed with bytes remaining' >&2
      exit 18
    fi
    [ ! -s "$out" ] || { echo 'partial file was not discarded' >&2; exit 99; } ;;
esac
`)
		result := fixture.run("1.2.3")
		Expect(result.exitCode).To(BeZero(), result.stderr)
		Expect(fixture.installedVersion()).To(Equal("1.2.3\n"))
	})

	It("preserves the existing executable and profile when both downloaders fail", func() {
		fixture := newInstallerDownloadFixture()
		fixture.curlResponse("printf 'HTTP/2 403\\r\\n\\r\\n' > \"$headers\"\necho 'curl: HTTP 403' >&2\nexit 22\n")
		fixture.wgetResponse("printf '  HTTP/1.1 403 Forbidden\\r\\n\\r\\n' >&2\nexit 8\n")
		Expect(os.MkdirAll(fixture.install, 0o755)).To(Succeed())
		writeExecutable(filepath.Join(fixture.install, "cmdshape"), "#!/bin/sh\nprintf '0.9.4\\n'\n")
		profile := filepath.Join(fixture.home, ".profile")
		Expect(os.WriteFile(profile, []byte("# existing profile\n"), 0o644)).To(Succeed())

		result := fixture.run("1.2.3")
		Expect(result.exitCode).NotTo(BeZero())
		Expect(result.stderr).To(ContainSubstring("archive:"))
		Expect(result.stderr).To(ContainSubstring("HTTP 403"))
		Expect(fixture.installedVersion()).To(Equal("0.9.4\n"))
		Expect(string(mustReadFile(profile))).To(Equal("# existing profile\n"))
		entries, err := os.ReadDir(fixture.install)
		Expect(err).NotTo(HaveOccurred())
		Expect(entries).To(HaveLen(1))
	})

	DescribeTable("rejecting invalid latest release redirects",
		func(redirect string) {
			fixture := newInstallerDownloadFixture()
			fixture.curlResponse("printf '%s\\r\\n' 'HTTP/2 302' " + shellQuoteArg("Location: "+redirect) + " '' > \"$headers\"\nexit 0\n")
			result := fixture.run("latest")
			Expect(result.exitCode).NotTo(BeZero())
			Expect(result.stderr).To(ContainSubstring("version lookup:"))
			Expect(filepath.Join(fixture.install, "cmdshape")).NotTo(BeAnExistingFile())
		},
		Entry("different repository", "https://github.com/other/project/releases/tag/1.2.3"),
		Entry("HTTP downgrade", "http://github.com/SuppieRK/cmdshape/releases/tag/1.2.3"),
		Entry("leading v", "https://github.com/SuppieRK/cmdshape/releases/tag/v1.2.3"),
		Entry("extra URL components", "https://github.com/SuppieRK/cmdshape/releases/tag/1.2.3?ignored=true"),
		Entry("ambiguous headers", "https://github.com/SuppieRK/cmdshape/releases/tag/1.2.3\r\nLocation: https://evil.invalid"),
	)

	It("rejects an HTTP downgrade from wget without using the redirected URL", func() {
		fixture := newInstallerDownloadFixture()
		fixture.isolateTools()
		fixture.wgetResponse("printf '  HTTP/1.1 302 Found\\r\\n  Location: http://unsafe.invalid/archive\\r\\n\\r\\n' >&2\nexit 8\n")
		result := fixture.run("1.2.3")
		Expect(result.exitCode).NotTo(BeZero())
		Expect(result.stderr).To(ContainSubstring("refused non-HTTPS"))
	})

	It("rejects a checksum mismatch after wget recovery", func() {
		fixture := newInstallerDownloadFixture()
		fixture.curlResponse("printf 'HTTP/2 403\\r\\n\\r\\n' > \"$headers\"\nexit 22\n")
		fixture.wgetResponse("")
		Expect(os.WriteFile(fixture.checksums, []byte(strings.Repeat("0", 64)+"  cmdshape_1.2.3_linux_amd64.zip\n"), 0o644)).To(Succeed())
		result := fixture.run("1.2.3")
		Expect(result.exitCode).NotTo(BeZero())
		Expect(result.stderr).To(ContainSubstring("checksum mismatch"))
		Expect(filepath.Join(fixture.install, "cmdshape")).NotTo(BeAnExistingFile())
	})

	It("stops a wget redirect loop after ten redirects", func() {
		fixture := newInstallerDownloadFixture()
		fixture.isolateTools()
		counter := shellQuoteTestPath(filepath.Join(fixture.root, "requests"))
		fixture.wgetResponse(`count=0
if [ -f ` + counter + ` ]; then count="$(cat ` + counter + `)"; fi
count=$((count + 1))
printf '%s' "$count" > ` + counter + `
[ "$count" -le 11 ] || exit 99
printf '  HTTP/1.1 302 Found\r\n  Location: https://github.com/loop\r\n\r\n' >&2
exit 8
`)
		result := fixture.run("1.2.3")
		Expect(result.exitCode).NotTo(BeZero())
		Expect(result.stderr).To(ContainSubstring("excessive redirects"))
		Expect(filepath.Join(fixture.install, "cmdshape")).NotTo(BeAnExistingFile())
	})

	It("does not recover an archive size-limit failure with another downloader", func() {
		fixture := newInstallerDownloadFixture()
		fixture.curlResponse("printf 'HTTP/2 200\\r\\n\\r\\n' > \"$headers\"\nexit 63\n")
		fixture.wgetResponse("")
		result := fixture.run("1.2.3")
		Expect(result.exitCode).NotTo(BeZero())
		Expect(result.stderr).To(ContainSubstring("archive: download exceeds"))
		Expect(filepath.Join(fixture.install, "cmdshape")).NotTo(BeAnExistingFile())
	})

	It("bounds oversized checksum responses downloaded by wget", func() {
		fixture := newInstallerDownloadFixture()
		fixture.isolateTools()
		fixture.wgetResponse("")
		Expect(os.WriteFile(fixture.checksums, []byte(strings.Repeat("0", 2*1024*1024+1)), 0o644)).To(Succeed())
		result := fixture.run("1.2.3")
		Expect(result.exitCode).NotTo(BeZero())
		Expect(result.stderr).To(ContainSubstring("checksums:"))
		Expect(filepath.Join(fixture.install, "cmdshape")).NotTo(BeAnExistingFile())
	})
})

type installerDownloadFixture struct {
	root, bin, install, home, script, archive, checksums, path string
}

func newInstallerDownloadFixture() installerDownloadFixture {
	root := GinkgoT().TempDir()
	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "install.sh"))
	Expect(err).NotTo(HaveOccurred())
	archive, checksums := makeInstallFixtures(root, "1.2.3")
	f := installerDownloadFixture{
		root: root, bin: filepath.Join(root, "bin"), install: filepath.Join(root, "install"),
		home: filepath.Join(root, "home"), script: script, archive: archive, checksums: checksums,
	}
	Expect(os.MkdirAll(f.bin, 0o755)).To(Succeed())
	Expect(os.MkdirAll(f.home, 0o755)).To(Succeed())
	writeExecutable(filepath.Join(f.bin, "uname"), "#!/bin/sh\ncase \"$1\" in -m) echo x86_64 ;; *) echo Linux ;; esac\n")
	writeExecutable(filepath.Join(f.bin, "wget"), "#!/bin/sh\necho 'wget unavailable in fixture' >&2\nexit 8\n")
	f.path = testPATH(f.bin, os.Getenv("PATH"))
	return f
}

func (f installerDownloadFixture) curlResponse(response string) {
	writeExecutable(filepath.Join(f.bin, "curl"), fmt.Sprintf(`#!/bin/sh
out=""
headers=""
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o|--output) out="$2"; shift 2 ;;
    -D|--dump-header) headers="$2"; shift 2 ;;
    https://*) url="$1"; shift ;;
    *) shift ;;
  esac
done
%s
printf 'HTTP/2 200\r\n\r\n' > "$headers"
case "$url" in
  https://github.com/SuppieRK/cmdshape/releases/download/1.2.3/cmdshape_checksums.txt) cp %s "$out" ;;
  https://github.com/SuppieRK/cmdshape/releases/download/1.2.3/cmdshape_1.2.3_linux_amd64.zip) cp %s "$out" ;;
  *) echo "unexpected URL: $url" >&2; exit 99 ;;
esac
`, response, shellQuoteTestPath(f.checksums), shellQuoteTestPath(f.archive)))
}

func (f installerDownloadFixture) run(version string) shellRunResult {
	return runInstallScript(f.script, f.root, map[string]string{
		"VERSION": version, "CMDSHAPE_INSTALL_DIR": f.install,
		"HOME": f.home, "PATH": f.path, "SHELL": "/bin/sh",
	})
}

func (f *installerDownloadFixture) isolateTools() {
	for _, name := range []string{"awk", "cat", "chmod", "cp", "grep", "mkdir", "mktemp", "mv", "rm", "sed", "sh", "sleep", "tr", "unzip", "wc", "sha256sum", "shasum"} {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		writeExecutable(filepath.Join(f.bin, name), "#!/bin/sh\nexec "+shellQuoteTestPath(path)+" \"$@\"\n")
	}
	f.path = testShellPath(f.bin)
}

func (f installerDownloadFixture) wgetResponse(response string) {
	writeExecutable(filepath.Join(f.bin, "wget"), fmt.Sprintf(`#!/bin/sh
out=""
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -O|--output-document) out="$2"; shift 2 ;;
    https://*) url="$1"; shift ;;
    *) shift ;;
  esac
done
%s
printf '  HTTP/1.1 200 OK\r\n\r\n' >&2
case "$url" in
  https://github.com/SuppieRK/cmdshape/releases/download/1.2.3/cmdshape_checksums.txt) cp %s "$out" ;;
  https://github.com/SuppieRK/cmdshape/releases/download/1.2.3/cmdshape_1.2.3_linux_amd64.zip) cp %s "$out" ;;
  *) echo "unexpected URL: $url" >&2; exit 99 ;;
esac
`, response, shellQuoteTestPath(f.checksums), shellQuoteTestPath(f.archive)))
}

func (f installerDownloadFixture) installedVersion() string {
	result := runInstallScriptSnippet("", shellQuoteTestPath(filepath.Join(f.install, "cmdshape"))+" --version\n", nil)
	Expect(result.exitCode).To(BeZero(), result.stderr)
	return result.stdout
}
