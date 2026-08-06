// Package opc 는 docx(OPC 컨테이너)를 zip 엔트리 단위로 다룬다.
//
// 핵심 계약: 수정되지 않은 엔트리는 압축을 풀지도, 다시 압축하지도 않는다.
// zip.File.OpenRaw 로 읽은 압축 데이터를 zip.Writer.CreateRaw 로 그대로 흘려보낸다.
// 그래서 "안 건드린 파트는 바이트 동일"이 증명이 아니라 구조로 보장된다.
package opc

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

type part struct {
	file    *zip.File
	header  zip.FileHeader
	raw     []byte // 압축된 원본 바이트
	content []byte // 압축 해제 캐시. content 자체는 nil 이 유효한 값일 수 있어 loaded 로 적재 여부를 따로 추적한다
	loaded  bool
	dirty   bool
}

// Package 는 열린 docx 하나를 나타낸다.
type Package struct {
	// Hash 는 입력 파일 전체 바이트의 sha256 이다 ("sha256:<hex>").
	// 패치의 낙관적 잠금이 이 값을 대조한다.
	Hash string

	src   []byte
	order []string // zip 원본 엔트리 순서
	parts map[string]*part
}

func Open(path string) (*Package, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return OpenBytes(b)
}

func OpenBytes(b []byte) (*Package, error) {
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return nil, fmt.Errorf("zip 열기 실패: %w", err)
	}
	sum := sha256.Sum256(b)
	p := &Package{
		Hash:  "sha256:" + hex.EncodeToString(sum[:]),
		src:   b,
		parts: make(map[string]*part, len(zr.File)),
	}
	for _, f := range zr.File {
		rc, err := f.OpenRaw()
		if err != nil {
			return nil, fmt.Errorf("%s: raw 열기 실패: %w", f.Name, err)
		}
		raw, err := io.ReadAll(rc)
		if err != nil {
			return nil, fmt.Errorf("%s: raw 읽기 실패: %w", f.Name, err)
		}
		p.order = append(p.order, f.Name)
		p.parts[f.Name] = &part{file: f, header: f.FileHeader, raw: raw}
	}
	return p, nil
}

// Source 는 원본 파일 바이트를 돌려준다. I1 검증에 쓴다.
func (p *Package) Source() []byte { return p.src }

// Names 는 zip 원본 순서의 엔트리 이름을 돌려준다.
func (p *Package) Names() []string {
	out := make([]string, len(p.order))
	copy(out, p.order)
	return out
}

// Part 는 엔트리의 압축 해제 내용을 돌려준다. 결과는 캐시된다.
func (p *Package) Part(name string) ([]byte, error) {
	pt, ok := p.parts[name]
	if !ok {
		return nil, fmt.Errorf("파트 없음: %s", name)
	}
	if pt.loaded {
		return pt.content, nil
	}
	rc, err := pt.file.Open()
	if err != nil {
		return nil, fmt.Errorf("%s: 열기 실패: %w", name, err)
	}
	defer rc.Close()
	c, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("%s: 읽기 실패: %w", name, err)
	}
	pt.content = c
	pt.loaded = true
	return c, nil
}

// Replace 는 엔트리 내용을 갈아끼운다. 해당 엔트리만 dirty 가 된다.
func (p *Package) Replace(name string, content []byte) error {
	pt, ok := p.parts[name]
	if !ok {
		return fmt.Errorf("파트 없음: %s", name)
	}
	pt.content = content
	pt.loaded = true
	pt.dirty = true
	return nil
}

// Write 는 컨테이너를 재작성한다.
// dirty 가 아닌 엔트리는 압축 데이터를 그대로 통과시킨다.
func (p *Package) Write(w io.Writer) error {
	zw := zip.NewWriter(w)
	for _, name := range p.order {
		pt := p.parts[name]
		fh := pt.header // 값 복사 — 원본 헤더를 건드리지 않는다

		if !pt.dirty {
			dst, err := zw.CreateRaw(&fh)
			if err != nil {
				return fmt.Errorf("%s: CreateRaw 실패: %w", name, err)
			}
			if _, err := dst.Write(pt.raw); err != nil {
				return fmt.Errorf("%s: raw 쓰기 실패: %w", name, err)
			}
			continue
		}

		// 재압축 대상이므로 원본의 CRC·크기를 지운다. Writer 가 다시 계산한다.
		fh.CRC32 = 0
		fh.CompressedSize, fh.CompressedSize64 = 0, 0
		fh.UncompressedSize, fh.UncompressedSize64 = 0, 0
		dst, err := zw.CreateHeader(&fh)
		if err != nil {
			return fmt.Errorf("%s: CreateHeader 실패: %w", name, err)
		}
		if _, err := dst.Write(pt.content); err != nil {
			return fmt.Errorf("%s: 쓰기 실패: %w", name, err)
		}
	}
	return zw.Close()
}

// Bytes 는 Write 결과를 메모리로 돌려준다.
func (p *Package) Bytes() ([]byte, error) {
	var buf bytes.Buffer
	if err := p.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
