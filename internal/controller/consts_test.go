/*
Copyright 2026 Timo Feddern.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

// Test-only constants for strings that would otherwise trip goconst.
// Kept in a single _test.go file so unit and envtest suites share them.
const (
	testNSMinecraft = "minecraft"

	testTemplateName = "tpl"

	testKeyMode = "MODE"
	testKeyMem  = "MEM"
	testKeyEULA = "EULA"
	testKeyMOTD = "MOTD"

	testEnvMemory = "MEMORY"

	testValTrue = "true"
)
