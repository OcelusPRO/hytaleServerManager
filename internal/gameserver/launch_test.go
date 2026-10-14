package gameserver

import (
	"slices"
	"testing"

	"hytale-server-manager/internal/settings"
)

func TestLaunchSpecForcesUTF8(t *testing.T) {
	spec := BuildLaunchSpec(LaunchInputs{Settings: settings.Defaults(), TmpDir: "/tmp/x"})
	for _, want := range []string{"-Dstdin.encoding=UTF-8", "-Dstdout.encoding=UTF-8"} {
		if !slices.Contains(spec.Args, want) {
			t.Errorf("missing %s in %v", want, spec.Args)
		}
	}
	if !slices.Contains(spec.Env, "LANG=C.UTF-8") {
		t.Errorf("missing UTF-8 locale in %v", spec.Env)
	}
	if i := slices.Index(spec.Args, "-jar"); i < slices.Index(spec.Args, "-Dstdin.encoding=UTF-8") {
		t.Error("JVM options must come before -jar")
	}
}
