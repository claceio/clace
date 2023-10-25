// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package dev

import (
	"fmt"
	"os"
	"path"
	"runtime"
	"strings"

	"github.com/claceio/clace/internal/app/util"
	"github.com/evanw/esbuild/pkg/api"
	"github.com/evanw/esbuild/pkg/cli"
)

type LibraryType string

const (
	ESModule LibraryType = "ecmascript_module"
	Library  LibraryType = "library"
)

const (
	LIB_PATH = "static/gen/lib"
	ESM_PATH = "static/gen/esm"
)

// JSLibrary handles the downloading for JS libraries and esbuild based bundling for ESM libraries
type JSLibrary struct {
	libType           LibraryType
	directUrl         string
	packageName       string
	version           string
	esbuildArgs       [10]string // use an array so that the struct can be used as key in the jsCache map
	sanitizedFileName string
}

func NewLibrary(url string) *JSLibrary {
	j := JSLibrary{
		libType:           Library,
		directUrl:         url,
		sanitizedFileName: sanitizeFileName(url),
	}
	return &j
}

func NewLibraryESM(packageName string, version string, esbuildArgs []string) *JSLibrary {
	args := [10]string{}
	if esbuildArgs != nil {
		copy(args[:], esbuildArgs)
	}

	j := JSLibrary{
		libType:           ESModule,
		packageName:       packageName,
		version:           version,
		esbuildArgs:       args,
		sanitizedFileName: sanitizeFileName(packageName) + "-" + version + ".js",
	}
	return &j
}

func (j *JSLibrary) Setup(dev *AppDev, sourceFS *util.WritableSourceFs, workFS *util.WorkFs) (string, error) {
	if j.libType == Library {
		targetFile := path.Join(LIB_PATH, j.sanitizedFileName)
		targetDir := path.Dir(targetFile)
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			return "", fmt.Errorf("error creating directory %s : %s", LIB_PATH, err)
		}
		if err := dev.downloadFile(j.directUrl, sourceFS, targetFile); err != nil {
			return "", fmt.Errorf("error downloading %s : %s", j.directUrl, err)
		}
		return targetFile, nil
	} else if j.libType == ESModule {
		return j.setupEsbuild(dev, sourceFS, workFS)
	} else {
		return "", fmt.Errorf("invalid library type : %s", j.libType)
	}
}

func (j *JSLibrary) setupEsbuild(dev *AppDev, sourceFS *util.WritableSourceFs, workFS *util.WorkFs) (string, error) {
	targetDir := path.Join(sourceFS.Root, ESM_PATH)
	targetFile := path.Join(targetDir, j.sanitizedFileName)

	sourceFile, err := j.generateSourceFile(workFS)
	if err != nil {
		return "", err
	}

	esbuildArgs := []string{sourceFile, "--bundle", "--format=esm"}

	args := []string{}
	for _, arg := range j.esbuildArgs {
		if arg == "" {
			break
		}
		args = append(args, arg)
	}
	esbuildArgs = append(esbuildArgs, args...)
	dev.Trace().Msgf("esbuild args : %v", esbuildArgs)

	// Parse the build options from the esbuild args
	options, err := cli.ParseBuildOptions(esbuildArgs)
	if err != nil {
		return "", fmt.Errorf("error parsing esbuild args : %s", err)
	}
	//options.AbsWorkingDir = sourceFS.Root this fails if the source dir is not absolute
	options.Outfile = targetFile

	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return "", fmt.Errorf("error creating directory %s : %s", targetDir, err)
	}

	if dev.systemConfig.NodePath != "" {
		if dev.systemConfig.NodePath == "disable" {
			dev.Warn().Msg("node_modules path is disabled, esbuild is not being run")
			return "", nil
		}

		// Add node paths to the esbuild options to customize the node_module location
		var nodePaths []string
		if runtime.GOOS == "windows" {
			nodePaths = strings.Split(dev.systemConfig.NodePath, ";")
		} else {
			nodePaths = strings.Split(dev.systemConfig.NodePath, ":")
		}
		options.NodePaths = nodePaths
	}

	// Run esbuild to generate the output file
	result := api.Build(options)
	dev.Trace().Msgf("esbuild options : %+v", options)
	if len(result.Errors) > 0 {
		// Return the target file name. The caller can check if the file exists to determine if the
		// setup was successful even though this step failed
		dev.Error().Msgf("error building %s : %v", j.packageName, result.Warnings)
		return targetFile, fmt.Errorf("error building %s : %v", j.packageName, result.Errors)
	}
	if len(result.Warnings) > 0 {
		dev.Warn().Msgf("warning building %s : %v", j.packageName, result.Warnings)
	}

	for _, file := range result.OutputFiles {
		target, _ := strings.CutPrefix(file.Path, sourceFS.Root)
		dev.Trace().Msgf("esbuild output file : %s %s", file.Path, target)
		err := sourceFS.Write(target, file.Contents)
		if err != nil {
			return "", fmt.Errorf("error writing esbuild output file %s : %s", file.Path, err)
		}
	}
	return options.Outfile, nil
}

func (j *JSLibrary) generateSourceFile(workFS *util.WorkFs) (string, error) {
	sourceFileName := j.sanitizedFileName
	sourceContent := fmt.Sprintf(`export * from "%s"`, j.packageName)
	if err := workFS.Write(sourceFileName, []byte(sourceContent)); err != nil {
		return "", fmt.Errorf("error writing source file %s : %s", sourceFileName, err)
	}
	return path.Join(workFS.Root, sourceFileName), nil
}

func sanitizeFileName(input string) string {
	output := path.Base(input)
	output = strings.ReplaceAll(output, "@", "")
	return output
}
