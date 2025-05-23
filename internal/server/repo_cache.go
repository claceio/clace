// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"cmp"
	"fmt"
	"os"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport" // for AuthMethod
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/go-git/go-git/v5/storage/memory"
)

type Repo struct {
	url    string
	branch string
	commit string
	auth   string
}

type CacheDir struct {
	dir           string
	commitMessage string
	hash          string
}

type RepoCache struct {
	server   *Server
	rootDir  string
	cache    map[Repo]CacheDir
	shaCache map[Repo]string // Cache for commit hashes
}

func NewRepoCache(server *Server) (*RepoCache, error) {
	tmpDir, err := os.MkdirTemp("", "clace_git_")
	if err != nil {
		return nil, err
	}
	return &RepoCache{
		server:   server,
		rootDir:  tmpDir,
		cache:    make(map[Repo]CacheDir),
		shaCache: make(map[Repo]string),
	}, nil
}

func (r *RepoCache) GetSha(sourceUrl, branch, gitAuth string) (string, error) {
	gitAuth = cmp.Or(gitAuth, r.server.config.Security.DefaultGitAuth)

	// Figure on which repo to clone
	repo, _, err := parseGithubUrl(sourceUrl, gitAuth)
	if err != nil {
		return "", err
	}

	// Check if we have the commit in cache
	if sha, ok := r.shaCache[Repo{repo, branch, "", gitAuth}]; ok {
		return sha, nil
	}

	var auth transport.AuthMethod
	if gitAuth != "" {
		// Auth is specified, load the key
		authEntry, err := r.server.loadGitKey(gitAuth)
		if err != nil {
			return "", err
		}
		r.server.Info().Msgf("Using git auth %s", authEntry.user)
		auth, err = ssh.NewPublicKeys(authEntry.user, authEntry.key, authEntry.password)
		if err != nil {
			return "", err
		}
	}

	sha, err := latestCommitSHA(repo, branch, auth)
	r.shaCache[Repo{repo, branch, "", gitAuth}] = sha
	return sha, nil
}

func latestCommitSHA(repoURL, branch string, auth transport.AuthMethod) (string, error) {
	remoteCfg := &config.RemoteConfig{
		Name: "origin",
		URLs: []string{repoURL},
	}
	remote := git.NewRemote(memory.NewStorage(), remoteCfg)

	refs, err := remote.List(&git.ListOptions{
		Auth: auth,
	})
	if err != nil {
		return "", fmt.Errorf("could not list remote refs: %w", err)
	}

	want := plumbing.NewBranchReferenceName(branch) // e.g. "refs/heads/main"
	for _, ref := range refs {
		if ref.Name() == want {
			return ref.Hash().String(), nil
		}
	}

	return "", fmt.Errorf("branch %q not found", branch)
}

func (r *RepoCache) CheckoutRepo(sourceUrl, branch, commit, gitAuth string) (string, string, string, string, error) {
	gitAuth = cmp.Or(gitAuth, r.server.config.Security.DefaultGitAuth)

	// Figure on which repo to clone
	repo, folder, err := parseGithubUrl(sourceUrl, gitAuth)
	if err != nil {
		return "", "", "", "", err
	}

	dir, ok := r.cache[Repo{repo, branch, commit, gitAuth}]
	if ok {
		return dir.dir, folder, dir.commitMessage, dir.hash, nil
	}

	cloneOptions := git.CloneOptions{
		URL: repo,
	}

	if commit == "" {
		// No commit id specified, checkout specified branch
		cloneOptions.ReferenceName = plumbing.NewBranchReferenceName(branch)
		cloneOptions.SingleBranch = true
		cloneOptions.Depth = 1
	}

	if gitAuth != "" {
		// Auth is specified, load the key
		authEntry, err := r.server.loadGitKey(gitAuth)
		if err != nil {
			return "", "", "", "", err
		}
		r.server.Info().Msgf("Using git auth %s", authEntry.user)
		auth, err := ssh.NewPublicKeys(authEntry.user, authEntry.key, authEntry.password)
		if err != nil {
			return "", "", "", "", err
		}
		cloneOptions.Auth = auth
	}

	targetPath, err := os.MkdirTemp(r.rootDir, "repo_")
	if err != nil {
		return "", "", "", "", err
	}

	// Configure the repo to Clone
	r.server.Info().Msgf("Cloning git repo %s to %s", repo, targetPath)
	gitRepo, err := git.PlainClone(targetPath, false, &cloneOptions)
	if err != nil {
		return "", "", "", "", fmt.Errorf("error checking out branch %s: %w", branch, err)
	}

	w, err := gitRepo.Worktree()
	if err != nil {
		return "", "", "", "", err
	}
	// Checkout specified hash
	options := git.CheckoutOptions{}
	if commit != "" {
		r.server.Info().Msgf("Checking out commit %s", commit)
		options.Hash = plumbing.NewHash(commit)
	} else {
		options.Branch = plumbing.NewBranchReferenceName(branch)
	}

	/* Sparse checkout seems to not be reliable with go-git
	if folder != "" {
		options.SparseCheckoutDirectories = []string{folder}
	}
	*/
	if err := w.Checkout(&options); err != nil {
		return "", "", "", "", fmt.Errorf("error checking out branch %s commit %s: %w", branch, commit, err)
	}

	ref, err := gitRepo.Head()
	if err != nil {
		return "", "", "", "", err
	}
	newCommit, err := gitRepo.CommitObject(ref.Hash())
	if err != nil {
		return "", "", "", "", err
	}

	// Save the repo in cache
	r.cache[Repo{repo, branch, commit, gitAuth}] = CacheDir{
		dir:           targetPath,
		commitMessage: newCommit.Message,
		hash:          newCommit.Hash.String(),
	}

	return targetPath, folder, newCommit.Message, newCommit.Hash.String(), nil
}

func (r *RepoCache) Cleanup() {
	if r.rootDir != "" {
		os.RemoveAll(r.rootDir)
		r.rootDir = ""
	}
}
