package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"

	"github.com/SONGYEONGSIN/pantograph/internal/opc"
	"github.com/SONGYEONGSIN/pantograph/internal/patch"
	"github.com/SONGYEONGSIN/pantograph/internal/tmpl"
)

func cmdTmpl(args []string) int {
	if len(args) == 0 {
		return die(exitInput, "사용법: panto tmpl extract|fill …")
	}
	switch args[0] {
	case "extract":
		return cmdTmplExtract(args[1:])
	case "fill":
		return cmdTmplFill(args[1:])
	default:
		return die(exitInput, "알 수 없는 하위 명령: %s", args[0])
	}
}

func cmdTmplExtract(args []string) int {
	var inputs []string
	var out, schemaPath string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-o":
			i++
			if i >= len(args) {
				return die(exitInput, "-o 뒤에 출력 경로가 필요하다")
			}
			out = args[i]
		case "--schema":
			i++
			if i >= len(args) {
				return die(exitInput, "--schema 뒤에 출력 경로가 필요하다")
			}
			schemaPath = args[i]
		default:
			inputs = append(inputs, args[i])
		}
	}
	if len(inputs) < 2 || out == "" || schemaPath == "" {
		return die(exitInput, "사용법: panto tmpl extract <a.docx> <b.docx> [...] -o <tmpl.docx> --schema <schema.json>")
	}

	pkgs := make([]*opc.Package, len(inputs))
	names := make([]string, len(inputs))
	for i, in := range inputs {
		p, err := opc.Open(in)
		if err != nil {
			return die(exitInternal, "%v", err)
		}
		pkgs[i] = p
		names[i] = filepath.Base(in)
	}

	tp, sch, errs, err := tmpl.Extract(pkgs, names)
	if err != nil {
		return die(exitInternal, "%v", err)
	}
	if len(errs) > 0 {
		if err := emit(patch.Result{OK: false, Errors: errs}); err != nil {
			return die(exitInternal, "%v", err)
		}
		return exitInput
	}

	if err := writeAtomic(out, tp.Write); err != nil {
		return die(exitInternal, "%v", err)
	}
	sb, err := json.MarshalIndent(sch, "", "  ")
	if err != nil {
		return die(exitInternal, "%v", err)
	}
	if err := writeAtomic(schemaPath, func(w io.Writer) error {
		_, err := w.Write(append(sb, '\n'))
		return err
	}); err != nil {
		return die(exitInternal, "%v", err)
	}
	if err := emit(patch.Result{OK: true}); err != nil {
		return die(exitInternal, "%v", err)
	}
	return exitOK
}

func cmdTmplFill(args []string) int {
	var in, dataPath, out, schemaPath string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-d":
			i++
			if i >= len(args) {
				return die(exitInput, "-d 뒤에 데이터 파일 경로가 필요하다")
			}
			dataPath = args[i]
		case "-o":
			i++
			if i >= len(args) {
				return die(exitInput, "-o 뒤에 출력 경로가 필요하다")
			}
			out = args[i]
		case "--schema":
			i++
			if i >= len(args) {
				return die(exitInput, "--schema 뒤에 스키마 경로가 필요하다")
			}
			schemaPath = args[i]
		default:
			if in != "" {
				return die(exitInput, "입력 파일이 둘 이상이다: %s, %s", in, args[i])
			}
			in = args[i]
		}
	}
	if in == "" || dataPath == "" || out == "" || schemaPath == "" {
		return die(exitInput, "사용법: panto tmpl fill <tmpl.docx> --schema <schema.json> -d <data.json> -o <out.docx>")
	}

	tp, err := opc.Open(in)
	if err != nil {
		return die(exitInternal, "%v", err)
	}
	sb, err := os.ReadFile(schemaPath)
	if err != nil {
		return die(exitInternal, "%v", err)
	}
	var sch tmpl.Schema
	if err := json.Unmarshal(sb, &sch); err != nil {
		return die(exitInput, "스키마 JSON 파싱 실패: %v", err)
	}
	db, err := os.ReadFile(dataPath)
	if err != nil {
		return die(exitInternal, "%v", err)
	}
	var data map[string]string
	if err := json.Unmarshal(db, &data); err != nil {
		return die(exitInput, "데이터 JSON 파싱 실패: %v", err)
	}

	errs, err := tmpl.Fill(tp, &sch, data)
	if err != nil {
		return die(exitInternal, "%v", err)
	}
	if len(errs) > 0 {
		if err := emit(patch.Result{OK: false, Errors: errs}); err != nil {
			return die(exitInternal, "%v", err)
		}
		return exitInput
	}

	if err := writeAtomic(out, tp.Write); err != nil {
		return die(exitInternal, "%v", err)
	}
	if err := emit(patch.Result{OK: true}); err != nil {
		return die(exitInternal, "%v", err)
	}
	return exitOK
}
