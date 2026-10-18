package snapshot

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

var memoryName = regexp.MustCompile(`^pagemap-([0-9]+|shmem-[0-9]+)\.img$`)

type memoryMap struct {
	Directory, Key string
	Saved          savedMap
	Data           *os.File
	DataBytes      int64
	Parent         *memoryMap
}

type memoryGraph struct {
	Root    string
	Maps    map[string]*memoryMap
	Data    map[string]*os.File
	Loading map[string]bool
}

func openMemoryGraph(head string) (*memoryGraph, error) {
	if filepath.Clean(head) != head || !strings.HasPrefix(head, "/experiment/") {
		return nil, fmt.Errorf("memory image graph is outside the owned experiment")
	}
	actual, err := filepath.EvalSymlinks(head)
	if err != nil || actual != head {
		return nil, fmt.Errorf("memory image head is not canonical: %v", err)
	}
	return &memoryGraph{Root: filepath.Dir(head), Maps: map[string]*memoryMap{},
		Data: map[string]*os.File{}, Loading: map[string]bool{}}, nil
}

func (graph *memoryGraph) close() error {
	var result error
	for _, file := range graph.Data {
		result = errors.Join(result, file.Close())
	}
	return result
}

func (graph *memoryGraph) load(directory, key string) (*memoryMap, error) {
	if !graphWithin(graph.Root, directory) ||
		!memoryName.MatchString("pagemap-"+key+".img") {
		return nil, fmt.Errorf("memory parent or image identity escaped its owned graph")
	}
	cache := directory + "/" + key
	if graph.Loading[directory] {
		return nil, fmt.Errorf("memory parent graph contains a cycle")
	}
	if saved := graph.Maps[cache]; saved != nil {
		return saved, nil
	}
	graph.Loading[directory] = true
	defer delete(graph.Loading, directory)
	path := filepath.Join(directory, "pagemap-"+key+".img")
	file, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	info, statErr := file.Stat()
	if statErr != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, fmt.Errorf("memory pagemap is not a regular file: %v", statErr)
	}
	saved, decodeErr := readSavedMap(file)
	if err := errors.Join(decodeErr, file.Close()); err != nil {
		return nil, err
	}
	memory := &memoryMap{Directory: directory, Key: key, Saved: saved}
	local, inherited := false, false
	var required int64
	for _, entry := range saved.Ranges {
		if entry.Flags&4 != 0 {
			local = true
			required = entry.Offset + int64(entry.Pages*4096)
		} else if entry.Flags&1 != 0 {
			inherited = true
		}
	}
	if local {
		name := filepath.Join(directory, fmt.Sprintf("pages-%d.img", saved.PageID))
		memory.Data = graph.Data[name]
		if memory.Data == nil {
			data, err := os.OpenFile(name, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
			if err != nil {
				return nil, err
			}
			graph.Data[name], memory.Data = data, data
		}
		info, err := memory.Data.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() < required {
			return nil, fmt.Errorf("memory payload is not complete: %s %v", name, err)
		}
		memory.DataBytes = info.Size()
	}
	if inherited {
		link, err := os.Readlink(filepath.Join(directory, "parent"))
		if err != nil || filepath.IsAbs(link) {
			return nil, fmt.Errorf("inherited memory has no relative owned parent: %v", err)
		}
		parent, err := filepath.EvalSymlinks(filepath.Join(directory, link))
		if err != nil {
			return nil, err
		}
		parentKey := key
		if !strings.HasPrefix(key, "shmem-") {
			alias, err := os.OpenFile(filepath.Join(directory, "lazy-parent-"+key+".bin"),
				os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
			if err == nil {
				var encoded [16]byte
				info, statErr := alias.Stat()
				_, readErr := io.ReadFull(alias, encoded[:])
				closeErr := alias.Close()
				pid := binary.LittleEndian.Uint64(encoded[8:])
				if statErr != nil || readErr != nil || closeErr != nil || !info.Mode().IsRegular() ||
					info.Size() != 16 || binary.LittleEndian.Uint64(encoded[:8]) != 0x4c415a59494e4832 ||
					pid == 0 || pid > 1<<31-1 {
					return nil, fmt.Errorf("invalid inherited memory image-PID alias")
				}
				parentKey = strconv.FormatUint(pid, 10)
			} else if !os.IsNotExist(err) {
				return nil, err
			}
		}
		memory.Parent, err = graph.load(parent, parentKey)
		if err != nil {
			return nil, err
		}
	}
	graph.Maps[cache] = memory
	return memory, nil
}

func (memory *memoryMap) span(address uint64, buffer []byte) (int, bool, error) {
	index := sort.Search(len(memory.Saved.Ranges), func(index int) bool {
		entry := memory.Saved.Ranges[index]
		return entry.Address+entry.Pages*4096 > address
	})
	if index == len(memory.Saved.Ranges) || memory.Saved.Ranges[index].Address > address {
		return 0, false, fmt.Errorf("memory address %#x is absent from %s", address, memory.Key)
	}
	entry := memory.Saved.Ranges[index]
	count := int(min(uint64(len(buffer)), entry.Address+entry.Pages*4096-address))
	if entry.Flags&4 != 0 {
		if memory.Data == nil {
			return 0, false, fmt.Errorf("local memory range has no complete data file")
		}
		offset := entry.Offset + int64(address-entry.Address)
		read, err := memory.Data.ReadAt(buffer[:count], offset)
		if err != nil || read != count {
			return 0, false, fmt.Errorf("read complete memory range: %d/%d %v", read, count, err)
		}
		return read, false, nil
	}
	if memory.Parent == nil || entry.Flags&1 == 0 {
		return 0, false, fmt.Errorf("inherited memory range has no resolved parent")
	}
	read, _, err := memory.Parent.span(address, buffer[:count])
	return read, true, err
}

func memoryCoverage(saved savedMap) [][2]uint64 {
	var result [][2]uint64
	for _, entry := range saved.Ranges {
		end := entry.Address + entry.Pages*4096
		if len(result) > 0 && result[len(result)-1][1] == entry.Address {
			result[len(result)-1][1] = end
		} else {
			result = append(result, [2]uint64{entry.Address, end})
		}
	}
	return result
}

// Both captures must describe the same frozen source. The second one is the
// complete original writer, never another inherited view used as its oracle.
func CompareOriginalMemory(candidate, original string) (_ map[string]any, resultErr error) {
	return compareMemoryPolicy(candidate, original, true)
}

// This certifies assembly against the already captured logical graph. It does
// not independently validate CRIU's source selection against an original writer.
func CompareCapturedMemory(candidate, captured string) (_ map[string]any, resultErr error) {
	return compareMemoryPolicy(candidate, captured, false)
}

func compareMemoryPolicy(candidate, original string, requireOriginal bool) (_ map[string]any, resultErr error) {
	left, err := openMemoryGraph(candidate)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, left.close()) }()
	right, err := openMemoryGraph(original)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, right.close()) }()
	names := func(directory string) ([]string, error) {
		entries, err := os.ReadDir(directory)
		if err != nil {
			return nil, err
		}
		var result []string
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Name(), "pagemap-") || !strings.HasSuffix(entry.Name(), ".img") {
				continue
			}
			match := memoryName.FindStringSubmatch(entry.Name())
			if match == nil || !entry.Type().IsRegular() {
				return nil, fmt.Errorf("unsupported original-writer pagemap entry")
			}
			result = append(result, match[1])
		}
		return result, nil
	}
	candidateMaps, err := names(candidate)
	if err != nil {
		return nil, err
	}
	originalMaps, err := names(original)
	if err != nil || len(originalMaps) == 0 || asJSON(candidateMaps) != asJSON(originalMaps) {
		return nil, fmt.Errorf("inherited and original-writer memory populations differ: %v", err)
	}
	first, second := make([]byte, 64<<10), make([]byte, 64<<10)
	var total, parentBytes int64
	for _, key := range originalMaps {
		current, err := left.load(candidate, key)
		if err != nil {
			return nil, err
		}
		control, err := right.load(original, key)
		if err != nil {
			return nil, err
		}
		if requireOriginal && control.Parent != nil {
			return nil, fmt.Errorf("memory byte oracle is not a complete original writer")
		}
		coverage := memoryCoverage(control.Saved)
		if asJSON(coverage) != asJSON(memoryCoverage(current.Saved)) {
			return nil, fmt.Errorf("memory virtual-address coverage differs for %s", key)
		}
		for _, extent := range coverage {
			for cursor := extent[0]; cursor < extent[1]; {
				limit := int(min(uint64(len(first)), extent[1]-cursor))
				count, inherited, err := current.span(cursor, first[:limit])
				if err != nil {
					return nil, err
				}
				read, _, err := control.span(cursor, second[:count])
				if err != nil || read <= 0 {
					return nil, fmt.Errorf("original-writer range unavailable: %v", err)
				}
				// Local and inherited writers may split the same address range
				// differently. Compare their common prefix and visit the rest
				// on the next iteration; no logical bytes are skipped.
				count = min(count, read)
				if !bytes.Equal(first[:count], second[:count]) {
					for index := 0; index < count; index++ {
						if first[index] != second[index] {
							return nil, fmt.Errorf("inherited memory differs from original writer: image=%s address=%#x compared_bytes=%d",
								key, cursor+uint64(index), total+int64(index))
						}
					}
				}
				total += int64(count)
				if inherited {
					parentBytes += int64(count)
				}
				cursor += uint64(count)
			}
		}
	}
	proof := map[string]any{
		"memory_maps": len(originalMaps), "compared_bytes": total, "inherited_bytes": parentBytes,
	}
	if requireOriginal {
		proof["all_original_writer_bytes_match"] = true
		proof["scope"] = "Complete logical memory ranges resolved through private parent graphs and image-PID aliases; no sampled byte comparison."
	} else {
		proof["all_captured_graph_bytes_match"] = true
		proof["reference_is_captured_graph"] = true
		proof["scope"] = "Every logical output byte equals the captured graph, resolving parent ranges and image-PID aliases. This is an assembly check, not an independent original-writer source oracle."
	}
	return proof, nil
}
