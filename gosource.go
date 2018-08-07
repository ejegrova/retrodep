// Copyright (C) 2018 Tim Waugh
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package backvendor

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/vcs"
)

func pathStartsWith(dir, prefix string) bool {
	plen := len(prefix)
	if len(dir) <= plen {
		return dir == prefix
	}

	return dir[:plen] == prefix && dir[plen] == filepath.Separator
}

type vendoredSearch struct {
	// Path to the "vendor" directory
	vendor string

	// Path to last project identified
	lastdir string

	// Vendored packages, indexed by Root
	vendored map[string]*vcs.RepoRoot
}

func (s *vendoredSearch) inLastDir(pth string) bool {
	return s.lastdir != "" && pathStartsWith(pth, s.lastdir)
}

func processVendoredSource(search *vendoredSearch, pth string) error {
	// For .go source files, see which directory they are in
	thisimport := filepath.Dir(pth[1+len(search.vendor):])
	reporoot, err := vcs.RepoRootForImportPath(thisimport, false)
	if err != nil {
		return err
	}

	// The project name is relative to the vendor dir
	search.vendored[reporoot.Root] = reporoot
	search.lastdir = filepath.Join(search.vendor, reporoot.Root)
	return nil
}

// GoSource represents a filesystem tree containing Go source code.
type GoSource string

// Topdir returns the top-level path of the filesystem tree.
func (src GoSource) Topdir() string {
	return string(src)
}

// Vendor returns the path to the vendored source code.
func (src GoSource) Vendor() string {
	return filepath.Join(src.Topdir(), "vendor")
}

// VendoredProjects return a map of project import names to information
// about those projects, including which version control system they use.
func (src GoSource) VendoredProjects() (map[string]*vcs.RepoRoot, error) {
	search := vendoredSearch{
		vendor:   src.Vendor(),
		vendored: make(map[string]*vcs.RepoRoot),
	}
	walkfn := func(pth string, info os.FileInfo, err error) error {
		if err != nil {
			// Stop on error
			return err
		}

		// Ignore paths within the last project we identified
		if search.inLastDir(pth) {
			return nil
		}

		// Ignore anything except Go source
		if !info.Mode().IsRegular() || !strings.HasSuffix(pth, ".go") {
			return nil
		}

		// Identify the project
		return processVendoredSource(&search, pth)
	}

	if _, err := os.Stat(src.Topdir()); err != nil {
		return nil, err
	}

	if _, err := os.Stat(search.vendor); err == nil {
		err = filepath.Walk(search.vendor, walkfn)
		if err != nil {
			return nil, err
		}
	}

	return search.vendored, nil
}

func matchFromRefs(hashes FileHashes, wt *WorkingTree, refs []string) (string, error) {
	for _, ref := range refs {
		match, err := wt.FileHashesAreSubset(hashes, ref)
		if err != nil {
			return "", err
		}
		if match {
			return ref, nil
		}
	}

	return "", ErrorVersionNotFound
}

// DescribeVendoredProject attempts to identify the tag in the version
// control system which corresponds to the vendored copy of the
// project.
func (src GoSource) DescribeVendoredProject(project *vcs.RepoRoot) (string, error) {
	wt, err := NewWorkingTree(project)
	if err != nil {
		return "", err
	}
	defer wt.Close()

	projectdir := filepath.Join(src.Vendor(), project.Root)
	hashes, err := NewFileHashes(wt.VCS.Cmd, projectdir)
	if err != nil {
		return "", err
	}

	// First try matching against tags for semantic versions
	tags, err := wt.SemVerTags()
	if err != nil {
		return "", err
	}

	match, err := matchFromRefs(hashes, wt, tags)
	if (err != nil && err != ErrorVersionNotFound) || match != "" {
		return match, err
	}

	// Next try each revision
	revs, err := wt.Revisions()
	if err != nil {
		return "", err
	}

	rev, err := matchFromRefs(hashes, wt, revs)
	if err != nil {
		return "", err
	}
	desc, err := wt.DescribeRevision(rev)
	if err != nil {
		return rev, nil
	}
	return desc, nil
}
