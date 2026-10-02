package lifecycle

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("installer smoke", func() {
	It("rejects an installed executable whose version differs from the release", func() {
		root := GinkgoT().TempDir()
		binary := filepath.Join(root, "cmdshape")
		writeExecutable(binary, "#!/bin/sh\nif [ \"$1\" = --version ]; then echo 9.9.9; exit 0; fi\nif [ \"$1\" = --raw ]; then shift; fi\nexec \"$@\"\n")
		script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "ci-smoke.sh"))
		Expect(err).NotTo(HaveOccurred())
		result := runInstallScriptSnippet("", "bash "+shellQuoteTestPath(script)+" "+shellQuoteTestPath(binary)+" 1.2.3\n", map[string]string{
			"PATH": testPATH(root, os.Getenv("PATH")), "RUNNER_OS": "Linux",
		})
		Expect(result.exitCode).NotTo(BeZero())
		Expect(result.stderr).To(ContainSubstring("expected version 1.2.3"))
	})

	It("executes the explicit installation even when a stale cmdshape is on PATH", func() {
		root := GinkgoT().TempDir()
		binary := filepath.Join(root, "installed cmdshape")
		writeExecutable(binary, "#!/bin/sh\nif [ \"$1\" = --version ]; then echo 1.2.3; exit 0; fi\nif [ \"$1\" = --raw ]; then shift; fi\nexec \"$@\"\n")
		writeExecutable(filepath.Join(root, "cmdshape"), "#!/bin/sh\necho 'stale executable must not run' >&2\nexit 99\n")
		script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "ci-smoke.sh"))
		Expect(err).NotTo(HaveOccurred())
		result := runInstallScriptSnippet("", "bash "+shellQuoteTestPath(script)+" "+shellQuoteTestPath(binary)+" 1.2.3\n", map[string]string{
			"PATH": testPATH(root, os.Getenv("PATH")), "RUNNER_OS": "Linux",
		})
		Expect(result.exitCode).To(BeZero(), result.stderr)
	})
})
