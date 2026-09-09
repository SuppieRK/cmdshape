package lifecycle

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("install script", func() {
	var scriptPath string

	BeforeEach(func() {
		if _, err := exec.LookPath("sh"); err != nil {
			Skip("sh unavailable: " + err.Error())
		}
		var err error
		scriptPath, err = filepath.Abs(filepath.Join("..", "..", "scripts", "install.sh"))
		Expect(err).NotTo(HaveOccurred())
	})

	DescribeTable("rejecting explicit versions that are not exact semantic versions",
		func(version string) {
			workspace := GinkgoT().TempDir()
			binDir := filepath.Join(workspace, "bin")
			Expect(os.MkdirAll(binDir, 0o755)).To(Succeed())
			writeExecutable(filepath.Join(binDir, "uname"), `#!/bin/sh
case "$1" in
  -m) printf 'x86_64\n' ;;
  *) printf 'Linux\n' ;;
esac
`)
			writeExecutable(filepath.Join(binDir, "curl"), "#!/bin/sh\nexit 99\n")
			writeExecutable(filepath.Join(binDir, "unzip"), "#!/bin/sh\nexit 99\n")

			result := runInstallScript(scriptPath, workspace, map[string]string{
				"VERSION":              version,
				"CMDSHAPE_INSTALL_DIR": filepath.Join(workspace, "install"),
				"HOME":                 filepath.Join(workspace, "home"),
				"PATH":                 testPATH(binDir, os.Getenv("PATH")),
			})

			Expect(result.exitCode).NotTo(BeZero())
			Expect(result.stderr).To(ContainSubstring("release version must be exact semantic version (X.Y.Z): " + version))
			Expect(result.stderr).NotTo(ContainSubstring("missing required command"))
		},
		Entry("leading v prefix", "v1.2.3"),
		Entry("missing patch component", "1.2"),
		Entry("prerelease suffix", "1.2.3-beta.1"),
		Entry("leading-zero major", "01.2.3"),
		Entry("leading-zero minor", "1.02.3"),
		Entry("leading-zero patch", "1.2.03"),
		Entry("embedded newline", "1.2\n.3"),
		Entry("trailing newline", "1.2.3\n"),
		Entry("component overflow", "18446744073709551616.2.3"),
	)

	It("downloads a cmdshape release archive and installs the renamed binary", func() {
		workspace := GinkgoT().TempDir()
		binDir := filepath.Join(workspace, "bin")
		assetPath, checksumPath := makeInstallFixtures(workspace, "1.2.3")
		Expect(os.MkdirAll(binDir, 0o755)).To(Succeed())

		writeExecutable(filepath.Join(binDir, "uname"), `#!/bin/sh
case "$1" in
  -m) printf 'x86_64\n' ;;
  *) printf 'Linux\n' ;;
esac
`)
		writeExecutable(filepath.Join(binDir, "curl"), fmt.Sprintf(`#!/bin/sh
out=""
headers=""
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o) out="$2"; shift 2 ;;
    --dump-header) headers="$2"; shift 2 ;;
    *) url="$1"; shift ;;
  esac
done
[ "$out" != - ] || out=/dev/stdout
printf 'HTTP/2 200\r\n\r\n' > "$headers"
case "$url" in
  *cmdshape_checksums.txt) cp %s "$out" ;;
  *) cp %s "$out" ;;
esac
`, shellQuoteTestPath(checksumPath), shellQuoteTestPath(assetPath)))
		home := filepath.Join(workspace, "home")
		result := runInstallScript(scriptPath, workspace, map[string]string{
			"VERSION":              "1.2.3",
			"CMDSHAPE_INSTALL_DIR": filepath.Join(home, ".local", "bin"),
			"HOME":                 home,
			"PATH":                 testPATH(binDir, os.Getenv("PATH")),
		})

		Expect(result.exitCode).To(BeZero(), result.stderr)
		Expect(result.stdout).To(ContainSubstring("Downloading https://github.com/SuppieRK/cmdshape/releases/download/1.2.3/cmdshape_1.2.3_linux_amd64.zip"))
		Expect(result.stdout).To(ContainSubstring("Installed binary cmdshape 1.2.3 to "))

		body, err := os.ReadFile(filepath.Join(home, ".local", "bin", "cmdshape"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(body)).To(ContainSubstring("printf '1.2.3"))
	})

	It("never invokes lifecycle migration or repair commands", func() {
		workspace := GinkgoT().TempDir()
		binDir := filepath.Join(workspace, "bin")
		installDir := filepath.Join(workspace, "install")
		homeDir := filepath.Join(workspace, "home")
		commandLog := filepath.Join(workspace, "command.log")
		assetPath, checksumPath := makeInstallFixtures(workspace, "0.9.0")
		Expect(os.MkdirAll(binDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(installDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(homeDir, 0o755)).To(Succeed())

		writeExecutable(filepath.Join(binDir, "uname"), `#!/bin/sh
case "$1" in
  -m) printf 'x86_64\n' ;;
  *) printf 'Linux\n' ;;
esac
`)
		writeExecutable(filepath.Join(binDir, "curl"), fmt.Sprintf(`#!/bin/sh
out=""
headers=""
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o) out="$2"; shift 2 ;;
    --dump-header) headers="$2"; shift 2 ;;
    *) url="$1"; shift ;;
  esac
done
[ "$out" != - ] || out=/dev/stdout
printf 'HTTP/2 200\r\n\r\n' > "$headers"
case "$url" in
  *cmdshape_checksums.txt) cp %s "$out" ;;
  *) cp %s "$out" ;;
esac
`, shellQuoteTestPath(checksumPath), shellQuoteTestPath(assetPath)))
		result := runInstallScript(scriptPath, workspace, map[string]string{
			"VERSION":                   "0.9.0",
			"CMDSHAPE_INSTALL_DIR":      installDir,
			"CMDSHAPE_INSTALL_TEST_LOG": commandLog,
			"HOME":                      homeDir,
			"PATH":                      testPATH(binDir, os.Getenv("PATH")),
		})

		Expect(result.exitCode).To(BeZero(), result.stderr)
		_, err := os.Stat(commandLog)
		Expect(err).To(MatchError(os.ErrNotExist))
		Expect(result.stdout).NotTo(ContainSubstring("migrate"))
		Expect(result.stdout).NotTo(ContainSubstring("repair"))
	})

	DescribeTable("rejecting unsafe install paths before downloading",
		func(installDir, expected string) {
			workspace := GinkgoT().TempDir()
			binDir := filepath.Join(workspace, "bin")
			curlMarker := filepath.Join(workspace, "curl-called")
			Expect(os.MkdirAll(binDir, 0o755)).To(Succeed())
			writeExecutable(filepath.Join(binDir, "uname"), "#!/bin/sh\ncase \"$1\" in -m) printf 'x86_64\\n' ;; *) printf 'Linux\\n' ;; esac\n")
			writeExecutable(filepath.Join(binDir, "curl"), "#!/bin/sh\nprintf called > "+shellQuoteTestPath(curlMarker)+"\nexit 99\n")
			writeExecutable(filepath.Join(binDir, "unzip"), "#!/bin/sh\nexit 99\n")

			result := runInstallScript(scriptPath, workspace, map[string]string{
				"VERSION":              "1.2.3",
				"CMDSHAPE_INSTALL_DIR": installDir,
				"HOME":                 filepath.Join(workspace, "home"),
				"PATH":                 testPATH(binDir, os.Getenv("PATH")),
			})

			Expect(result.exitCode).NotTo(BeZero())
			Expect(result.stderr).To(ContainSubstring(expected))
			_, err := os.Stat(curlMarker)
			Expect(err).To(MatchError(os.ErrNotExist))
		},
		Entry("relative path", "relative/bin", "must be an absolute path"),
		Entry("line feed", filepath.Join("/tmp", "bad\npath"), "must not contain CR or LF"),
		Entry("carriage return", filepath.Join("/tmp", "bad\rpath"), "must not contain CR or LF"),
		Entry("PATH separator", filepath.Join("/tmp", "bad:path"), "cannot be represented safely in PATH"),
	)

	It("rejects an install symlink whose resolved path is unsafe", func() {
		if runtime.GOOS == "windows" {
			Skip("symlink creation is not generally available on Windows runners")
		}
		workspace := GinkgoT().TempDir()
		binDir := filepath.Join(workspace, "bin")
		target := filepath.Join(workspace, "target:dir")
		requested := filepath.Join(workspace, "requested")
		curlMarker := filepath.Join(workspace, "curl-called")
		Expect(os.MkdirAll(binDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(target, 0o755)).To(Succeed())
		Expect(os.Symlink(target, requested)).To(Succeed())
		writeExecutable(filepath.Join(binDir, "uname"), "#!/bin/sh\ncase \"$1\" in -m) printf 'x86_64\\n' ;; *) printf 'Linux\\n' ;; esac\n")
		writeExecutable(filepath.Join(binDir, "curl"), "#!/bin/sh\nprintf called > "+shellQuoteTestPath(curlMarker)+"\nexit 99\n")
		writeExecutable(filepath.Join(binDir, "unzip"), "#!/bin/sh\nexit 99\n")

		result := runInstallScript(scriptPath, workspace, map[string]string{
			"VERSION": "1.2.3", "CMDSHAPE_INSTALL_DIR": requested,
			"HOME": filepath.Join(workspace, "home"), "PATH": testPATH(binDir, os.Getenv("PATH")),
		})

		Expect(result.exitCode).NotTo(BeZero())
		Expect(result.stderr).To(ContainSubstring("cannot be represented safely in PATH"))
		Expect(curlMarker).NotTo(BeAnExistingFile())
	})

	It("rejects an oversized checksum response", func() {
		workspace := GinkgoT().TempDir()
		binDir := filepath.Join(workspace, "bin")
		installDir := filepath.Join(workspace, "install")
		assetPath, _ := makeInstallFixtures(workspace, "1.2.3")
		oversized := filepath.Join(workspace, "oversized-checksums.txt")
		Expect(os.WriteFile(oversized, bytes.Repeat([]byte{'x'}, 1048577), 0o644)).To(Succeed())
		Expect(os.MkdirAll(binDir, 0o755)).To(Succeed())
		writeExecutable(filepath.Join(binDir, "uname"), "#!/bin/sh\ncase \"$1\" in -m) printf 'x86_64\\n' ;; *) printf 'Linux\\n' ;; esac\n")
		writeExecutable(filepath.Join(binDir, "curl"), fmt.Sprintf(`#!/bin/sh
out=""
headers=""
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in -o) out="$2"; shift 2 ;; --dump-header) headers="$2"; shift 2 ;; *) url="$1"; shift ;; esac
done
[ "$out" != - ] || out=/dev/stdout
printf 'HTTP/2 200\r\n\r\n' > "$headers"
case "$url" in *cmdshape_checksums.txt) cp %s "$out" ;; *) cp %s "$out" ;; esac
`, shellQuoteTestPath(oversized), shellQuoteTestPath(assetPath)))

		result := runInstallScript(scriptPath, workspace, map[string]string{
			"VERSION": "1.2.3", "CMDSHAPE_INSTALL_DIR": installDir,
			"HOME": filepath.Join(workspace, "home"), "PATH": testPATH(binDir, os.Getenv("PATH")),
		})

		Expect(result.exitCode).NotTo(BeZero())
		Expect(result.stderr).To(ContainSubstring("download exceeds 1048576 bytes"))
	})

	It("fails an unwritable requested directory before downloading", func() {
		workspace := GinkgoT().TempDir()
		binDir := filepath.Join(workspace, "bin")
		installDir := filepath.Join(workspace, "install")
		curlMarker := filepath.Join(workspace, "curl-called")
		Expect(os.MkdirAll(binDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(installDir, 0o755)).To(Succeed())
		writeExecutable(filepath.Join(binDir, "uname"), "#!/bin/sh\ncase \"$1\" in -m) printf 'x86_64\\n' ;; *) printf 'Linux\\n' ;; esac\n")
		writeExecutable(filepath.Join(binDir, "curl"), "#!/bin/sh\nprintf called > "+shellQuoteTestPath(curlMarker)+"\nexit 99\n")
		writeExecutable(filepath.Join(binDir, "unzip"), "#!/bin/sh\nexit 99\n")
		writeExecutable(filepath.Join(binDir, "mktemp"), "#!/bin/sh\nexit 71\n")

		result := runInstallScript(scriptPath, workspace, map[string]string{
			"VERSION":              "1.2.3",
			"CMDSHAPE_INSTALL_DIR": installDir,
			"HOME":                 filepath.Join(workspace, "home"),
			"PATH":                 testPATH(binDir, os.Getenv("PATH")),
		})

		Expect(result.exitCode).NotTo(BeZero())
		Expect(result.stderr).To(ContainSubstring("install directory is not writable"))
		_, err := os.Stat(curlMarker)
		Expect(err).To(MatchError(os.ErrNotExist))
	})

	It("uses an unpredictable same-directory replacement name", func() {
		body, err := os.ReadFile(scriptPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(body)).To(ContainSubstring(`mktemp "$INSTALL_DIR/.${BIN_NAME}.new.XXXXXX"`))
		Expect(string(body)).NotTo(ContainSubstring(`.${BIN_NAME}.new.$$`))
	})

	Describe("sourced helper behavior", func() {
		var functionPrefix string

		BeforeEach(func() {
			body, err := os.ReadFile(scriptPath)
			Expect(err).NotTo(HaveOccurred())
			functionPrefix = installScriptFunctionPrefix(string(body))
		})

		It("uses the cmdshape install directory variable", func() {
			result := runInstallScriptSnippet(
				functionPrefix,
				"printf 'requested=%s\\n' \"$REQUESTED_INSTALL_DIR\"\n",
				map[string]string{"CMDSHAPE_INSTALL_DIR": "/tmp/cmdshape-bin"},
			)

			Expect(result.exitCode).To(BeZero(), result.stderr)
			Expect(result.stdout).To(ContainSubstring("requested=/tmp/cmdshape-bin"))
		})

		It("normalizes a native Windows install directory before validating it", func() {
			workspace := GinkgoT().TempDir()
			binDir := filepath.Join(workspace, "bin")
			installDir := filepath.Join(workspace, "windows-install")
			Expect(os.MkdirAll(binDir, 0o755)).To(Succeed())
			Expect(os.MkdirAll(installDir, 0o755)).To(Succeed())
			writeExecutable(filepath.Join(binDir, "cygpath"), "#!/bin/sh\nprintf '%s\\n' "+shellQuoteTestPath(testShellPath(installDir))+"\n")

			result := runInstallScriptSnippet(
				functionPrefix,
				fmt.Sprintf("OS=windows\nREQUESTED_INSTALL_DIR=%s\nresolved=$(choose_install_dir)\nprintf 'resolved=%%s\\n' \"$resolved\"\n", shellQuoteArg(`D:\tools\cmdshape`)),
				map[string]string{
					"PATH": testPATH(binDir, os.Getenv("PATH")),
				},
			)

			Expect(result.exitCode).To(BeZero(), result.stderr)
			Expect(strings.TrimPrefix(strings.TrimSpace(result.stdout), "resolved=")).To(Equal(testShellPath(resolvedPath(installDir))))
		})

		DescribeTable("validating release versions",
			func(input, expected string, wantSuccess bool) {
				result := runInstallScriptSnippet(functionPrefix, fmt.Sprintf("result=$(validate_release_version %s)\nprintf 'status=%%s value=%%s\\n' \"$?\" \"$result\"\n", shellQuoteArg(input)), nil)

				if wantSuccess {
					Expect(result.exitCode).To(BeZero(), result.stderr)
					Expect(result.stdout).To(ContainSubstring("status=0 value=" + expected))
					return
				}
				Expect(result.exitCode).NotTo(BeZero())
			},
			Entry("accepts exact semantic versions", "1.2.3", "1.2.3", true),
			Entry("rejects versions with v prefix", "v1.2.3", "", false),
			Entry("rejects prerelease versions", "1.2.3-rc1", "", false),
			Entry("rejects leading-zero major", "01.2.3", "", false),
			Entry("rejects leading-zero minor", "1.02.3", "", false),
			Entry("rejects leading-zero patch", "1.2.03", "", false),
			Entry("rejects embedded newline", "1.2\n.3", "", false),
			Entry("rejects uint64 overflow", "18446744073709551616.2.3", "", false),
		)

		It("appends a PATH export only once", func() {
			workspace := GinkgoT().TempDir()
			profilePath := filepath.Join(workspace, ".profile")

			result := runInstallScriptSnippet(functionPrefix, fmt.Sprintf("append_path_export_once %s %s\nappend_path_export_once %s %s\ncat %s\n", shellQuoteTestPath(profilePath), shellQuoteArg("/tmp/cmdshape-bin"), shellQuoteTestPath(profilePath), shellQuoteArg("/tmp/cmdshape-bin"), shellQuoteTestPath(profilePath)), nil)

			Expect(result.exitCode).To(BeZero(), result.stderr)
			Expect(strings.Count(result.stdout, "# added by cmdshape installer")).To(Equal(1))
			Expect(strings.Count(result.stdout, `export PATH='/tmp/cmdshape-bin':"$PATH"`)).To(Equal(1))
		})

		It("prepends an install directory that is present later in PATH", func() {
			workspace := GinkgoT().TempDir()
			oldDir := filepath.Join(workspace, "old")
			installDir := filepath.Join(workspace, "install")
			homeDir := filepath.Join(workspace, "home")
			Expect(os.MkdirAll(oldDir, 0o755)).To(Succeed())
			Expect(os.MkdirAll(installDir, 0o755)).To(Succeed())
			Expect(os.MkdirAll(homeDir, 0o755)).To(Succeed())

			result := runInstallScriptSnippet(
				functionPrefix,
				fmt.Sprintf("update_path_if_needed %s\nupdate_path_if_needed %s\ncat %s\n", shellQuoteTestPath(installDir), shellQuoteTestPath(installDir), shellQuoteTestPath(filepath.Join(homeDir, ".profile"))),
				map[string]string{
					"HOME":  homeDir,
					"PATH":  testPATH(oldDir, testPATH(installDir, os.Getenv("PATH"))),
					"SHELL": "/bin/sh",
				},
			)

			Expect(result.exitCode).To(BeZero(), result.stderr)
			quoted := strings.ReplaceAll(testShellPath(installDir), "'", "'\\''")
			Expect(strings.Count(result.stdout, "export PATH='"+quoted+"':\"$PATH\"")).To(Equal(1))
		})

		DescribeTable("rejecting ambiguous or malformed checksum entries",
			func(contents, expected string) {
				workspace := GinkgoT().TempDir()
				checksumsPath := filepath.Join(workspace, "checksums.txt")
				assetPath := filepath.Join(workspace, "asset.zip")
				Expect(os.WriteFile(checksumsPath, []byte(contents), 0o644)).To(Succeed())
				Expect(os.WriteFile(assetPath, []byte("asset"), 0o644)).To(Succeed())

				result := runInstallScriptSnippet(
					functionPrefix,
					fmt.Sprintf("verify_download_checksum %s asset.zip %s\n", shellQuoteTestPath(checksumsPath), shellQuoteTestPath(assetPath)),
					nil,
				)

				Expect(result.exitCode).NotTo(BeZero())
				Expect(result.stderr).To(ContainSubstring(expected))
			},
			Entry("duplicate exact filename", strings.Repeat("a", 64)+"  asset.zip\n"+strings.Repeat("b", 64)+"  asset.zip\n", "duplicate checksum"),
			Entry("short digest", "deadbeef  asset.zip\n", "exactly 64 hex digits"),
			Entry("non-hex digest", strings.Repeat("z", 64)+"  asset.zip\n", "not hexadecimal"),
		)

		DescribeTable("requiring staged version output to be exact X.Y.Z plus one LF",
			func(output string, accepted bool) {
				if runtime.GOOS == "windows" {
					Skip("uses a unix shell script as the staged executable")
				}
				workspace := GinkgoT().TempDir()
				candidate := filepath.Join(workspace, "cmdshape")
				Expect(os.MkdirAll(filepath.Join(workspace, "tmp"), 0o755)).To(Succeed())
				writeExecutable(candidate, fmt.Sprintf("#!/bin/sh\nprintf %s %s\n", shellQuoteArg("%s"), shellQuoteArg(output)))

				result := runInstallScriptSnippet(
					functionPrefix,
					fmt.Sprintf("TMP_DIR=%s\nvalidate_staged_binary %s 1.2.3\n", shellQuoteTestPath(filepath.Join(workspace, "tmp")), shellQuoteTestPath(candidate)),
					nil,
				)

				if accepted {
					Expect(result.exitCode).To(BeZero(), result.stderr)
					return
				}
				Expect(result.exitCode).NotTo(BeZero())
				Expect(result.stderr).To(ContainSubstring("staged binary version mismatch"))
			},
			Entry("accepts one LF", "1.2.3\n", true),
			Entry("rejects no delimiter", "1.2.3", false),
			Entry("rejects CRLF", "1.2.3\r\n", false),
			Entry("rejects extra records", "1.2.3\nextra\n", false),
		)

		It("quotes the install path as literal profile data", func() {
			workspace := GinkgoT().TempDir()
			profilePath := filepath.Join(workspace, ".profile")
			installDir := "/tmp/space dir/quo'te/$cash/`tick`/-leading"
			result := runInstallScriptSnippet(functionPrefix, fmt.Sprintf("append_path_export_once %s %s\nappend_path_export_once %s %s\ncat %s\n", shellQuoteTestPath(profilePath), shellQuoteArg(installDir), shellQuoteTestPath(profilePath), shellQuoteArg(installDir), shellQuoteTestPath(profilePath)), nil)

			Expect(result.exitCode).To(BeZero(), result.stderr)
			quoted := strings.ReplaceAll(installDir, "'", "'\\''")
			Expect(strings.Count(result.stdout, "export PATH='"+quoted+"':\"$PATH\"")).To(Equal(1))
		})
	})
})

type shellRunResult struct {
	stdout   string
	stderr   string
	exitCode int
}

func runInstallScript(scriptPath, workdir string, env map[string]string) shellRunResult {
	if runtime.GOOS == "windows" {
		for _, key := range []string{"CMDSHAPE_INSTALL_DIR", "CMDSHAPE_INSTALL_TEST_LOG", "HOME"} {
			if env[key] != "" {
				env[key] = testShellPath(env[key])
			}
		}
	}
	cmd := exec.Command("sh", scriptPath)
	cmd.Dir = workdir
	cmd.Env = withEnv(env)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	exitCode := 0
	if err := cmd.Run(); err != nil {
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			exitCode = exitErr.ExitCode()
		} else {
			Fail(fmt.Sprintf("run install script: %v stderr=%s", err, stderr.String()))
		}
	}
	return shellRunResult{stdout: stdout.String(), stderr: stderr.String(), exitCode: exitCode}
}

func runInstallScriptSnippet(prefix, snippet string, env map[string]string) shellRunResult {
	workspace := GinkgoT().TempDir()
	script := prefix + "\n" + snippet
	scriptPath := filepath.Join(workspace, "snippet.sh")
	Expect(os.WriteFile(scriptPath, []byte(script), 0o755)).To(Succeed())
	return runInstallScript(scriptPath, workspace, env)
}

func installScriptFunctionPrefix(body string) string {
	const marker = "OS=\"$(uname -s | tr '[:upper:]' '[:lower:]')\""
	before, _, found := strings.Cut(body, marker)
	Expect(found).To(BeTrue())
	return before
}

func makeInstallFixtures(root, version string) (string, string) {
	assetName := fmt.Sprintf("cmdshape_%s_linux_amd64.zip", version)
	assetPath := filepath.Join(root, assetName)
	file, err := os.Create(assetPath)
	Expect(err).NotTo(HaveOccurred())
	zipWriter := zip.NewWriter(file)
	entry, err := zipWriter.Create("cmdshape")
	Expect(err).NotTo(HaveOccurred())
	_, err = entry.Write([]byte(
		"#!/bin/sh\n" +
			"if [ \"$1\" = \"--version\" ]; then printf '" + version + "\\n'; exit 0; fi\n" +
			"if [ -n \"${CMDSHAPE_INSTALL_TEST_LOG:-}\" ]; then printf '%s\\n' \"$*\" >> \"$CMDSHAPE_INSTALL_TEST_LOG\"; fi\n",
	))
	Expect(err).NotTo(HaveOccurred())
	Expect(zipWriter.Close()).To(Succeed())
	Expect(file.Close()).To(Succeed())

	checksumPath := filepath.Join(root, "cmdshape_checksums.txt")
	sum := sha256.Sum256(mustReadFile(assetPath))
	Expect(os.WriteFile(checksumPath, fmt.Appendf(nil, "%x  ./%s\n", sum, assetName), 0o644)).To(Succeed())
	return assetPath, checksumPath
}

func mustReadFile(path string) []byte {
	body, err := os.ReadFile(path)
	Expect(err).NotTo(HaveOccurred())
	return body
}

func withEnv(overrides map[string]string) []string {
	base := os.Environ()
	for key, value := range overrides {
		prefix := key + "="
		replaced := false
		for i, entry := range base {
			if strings.HasPrefix(entry, prefix) {
				base[i] = prefix + value
				replaced = true
				break
			}
		}
		if !replaced {
			base = append(base, prefix+value)
		}
	}
	return base
}

func writeExecutable(path, body string) {
	Expect(os.WriteFile(path, []byte(body), 0o755)).To(Succeed())
}

func testPATH(prefix, current string) string {
	if current == "" {
		return prefix
	}
	if runtime.GOOS == "windows" {
		cmd := exec.Command("sh", "-lc", `printf %s "$PATH"`)
		path, err := cmd.Output()
		Expect(err).NotTo(HaveOccurred())
		return testShellPath(prefix) + ":" + string(path)
	}
	return prefix + string(os.PathListSeparator) + current
}

func shellQuoteTestPath(path string) string {
	return shellQuoteArg(testShellPath(path))
}

func testShellPath(path string) string {
	if runtime.GOOS != "windows" {
		return path
	}
	slashed := filepath.ToSlash(path)
	volume := filepath.VolumeName(path)
	if len(volume) == 2 && volume[1] == ':' {
		return "/" + strings.ToLower(volume[:1]) + strings.TrimPrefix(slashed, volume)
	}
	return slashed
}
