package wechat

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func largeEntryUnpacker(t *testing.T) *Unpacker {
	t.Helper()
	root := t.TempDir()
	item := &WxapkgItem{UnpackSavePath: root}
	options := &UnpackOptions{SavePath: root}
	return NewUnpacker(item, options)
}

func largeEntryArchive(t *testing.T, name string, body []byte) ([]byte, uint32) {
	t.Helper()
	var data bytes.Buffer
	write := func(value interface{}) {
		t.Helper()
		if err := binary.Write(&data, binary.BigEndian, value); err != nil {
			t.Fatalf("write archive field: %v", err)
		}
	}

	write(uint8(0xBE))
	write(uint32(0))
	write(uint32(0))
	write(uint32(0))
	write(uint8(0xED))
	write(uint32(1))
	nameBytes := []byte(name)
	write(uint32(len(nameBytes)))
	if _, err := data.Write(nameBytes); err != nil {
		t.Fatalf("write archive name: %v", err)
	}
	offset := uint32(data.Len() + 8)
	write(offset)
	write(uint32(len(body)))
	if _, err := data.Write(body); err != nil {
		t.Fatalf("write archive body: %v", err)
	}
	return data.Bytes(), offset
}

func TestLargeArchiveEntry(t *testing.T) {
	body := bytes.Repeat([]byte("x"), 12777990) // wxmini-kit issue #2
	data, _ := largeEntryArchive(t, "/subpackages/main/game.js", body)
	t.Run("extract", func(t *testing.T) {
		u := largeEntryUnpacker(t)
		source := filepath.Join(t.TempDir(), "_subpackages_main_.wxapkg")
		if err := os.WriteFile(source, data, 0600); err != nil {
			t.Fatal(err)
		}
		files, err := u.analyze(data, source, source)
		if err != nil {
			t.Fatal(err)
		}
		if len(files) != 1 {
			t.Fatalf("got %d files", len(files))
		}
		if err := u.unpackOne(files[0]); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(files[0].savePath)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, body) {
			t.Fatal("extracted content differs")
		}
	})
	t.Run("metadata index", func(t *testing.T) {
		entries, err := parseWxapkgIndex(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || entries[0].size != uint32(len(body)) {
			t.Fatalf("unexpected entries: %+v", entries)
		}
	})
	t.Run("truncated", func(t *testing.T) {
		truncated := data[:len(data)-1]
		if _, err := largeEntryUnpacker(t).analyze(truncated, "archive", "archive"); err == nil {
			t.Fatal("accepted truncated entry")
		}
		if _, err := parseWxapkgIndex(bytes.NewReader(truncated), int64(len(truncated))); err == nil {
			t.Fatal("accepted truncated index entry")
		}
	})
}
