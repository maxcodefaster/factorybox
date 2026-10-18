package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/checkpoint-restore/go-criu/v6/crit"
	"github.com/checkpoint-restore/go-criu/v6/crit/images"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/encoding/protowire"
)

// The pinned Go schema names field 2 nr_pages. This CRIU revision renamed
// it compat_nr_pages and stores the 64-bit count in field 5. Decode that
// preserved unknown field rather than silently reporting the legacy zero.
func pageCount(mapping *images.PagemapEntry) (uint64, error) {
	count := uint64(mapping.GetNrPages())
	unknown := mapping.ProtoReflect().GetUnknown()
	found := false
	for len(unknown) > 0 {
		number, kind, n := protowire.ConsumeTag(unknown)
		if n < 0 {
			return 0, protowire.ParseError(n)
		}
		unknown = unknown[n:]
		if number == 5 {
			if kind != protowire.VarintType || found {
				return 0, fmt.Errorf("invalid or repeated 64-bit pagemap page count")
			}
			value, n := protowire.ConsumeVarint(unknown)
			if n < 0 {
				return 0, protowire.ParseError(n)
			}
			count, found = value, true
			unknown = unknown[n:]
			continue
		}
		n = protowire.ConsumeFieldValue(number, kind, unknown)
		if n < 0 {
			return 0, protowire.ParseError(n)
		}
		unknown = unknown[n:]
	}
	if count == 0 {
		return 0, fmt.Errorf("pagemap range has no pages")
	}
	return count, nil
}

// Read actual native images with the pinned go-criu decoder. This command
// does not modify images or run a workload; parent counts verify inheritance.
func run() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("expected owned native archive directory")
	}
	root := os.Args[1]
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		name := entry.Name()
		if name != "stats-dump" && name != "stats-restore" && name != "inventory.img" && name != "pstree.img" && name != "files.img" && !strings.HasPrefix(name, "mm-") && !strings.HasPrefix(name, "pagemap-") && !strings.HasPrefix(name, "core-") {
			return nil
		}
		image, err := crit.New(path, "", "", false, true).Decode()
		if err != nil {
			return fmt.Errorf("decode %s: %w", path, err)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		output := map[string]any{"path": relative, "magic": image.Magic, "entries": len(image.Entries)}
		if strings.HasPrefix(name, "pagemap-") {
			var pages, inherited uint64
			ranges := []map[string]any{}
			for _, entry := range image.Entries {
				if mapping, ok := entry.Message.(*images.PagemapEntry); ok {
					count, err := pageCount(mapping)
					if err != nil {
						return fmt.Errorf("decode page count %s: %w", path, err)
					}
					pages += count
					inParent := mapping.GetInParent() || mapping.GetFlags()&1 != 0
					ranges = append(ranges, map[string]any{"start": mapping.GetVaddr(), "pages": count, "in_parent": inParent, "flags": mapping.GetFlags()})
					if inParent {
						inherited += count
					}
				}
			}
			output["pages"], output["parent_pages"] = pages, inherited
			output["ranges"] = ranges
		} else {
			messages := []json.RawMessage{}
			for _, entry := range image.Entries {
				b, err := protojson.Marshal(entry.Message)
				if err != nil {
					return err
				}
				messages = append(messages, b)
			}
			output["data"] = messages
		}
		return json.NewEncoder(os.Stdout).Encode(output)
	})
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
