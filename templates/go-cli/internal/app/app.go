// Package app は {{NAME}} の本体。main から切り離してテストできるようにする。
//
// 守ること: 重要な情報を持たない・送らない (DATA.md)。
// 保存するなら os.UserConfigDir() の下に、このツールの設定だけを、権限 0600 で。
package app

import (
	"errors"
	"flag"
	"fmt"
	"io"
)

// Version は goreleaser が埋める。
var Version = "dev"

// Main は main の中身。終了コードを返す (0: 成功、1: 失敗)。
func Main(args []string, stdout, stderr io.Writer) int {
	if err := Run(args, stdout); err != nil {
		_, _ = fmt.Fprintln(stderr, "{{NAME}}:", err) // 書けなくても終了コードで伝わる
		return 1
	}
	return 0
}

// Run は引数を受け取って結果を out に書く。
func Run(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("{{NAME}}", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	showVersion := fs.Bool("version", false, "バージョンを表示する")
	limit := fs.Int("max", 3, "カウンターの上限 (例)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("引数: %w", err)
	}
	if *showVersion {
		_, err := fmt.Fprintln(out, Version)
		return err
	}
	if fs.NArg() > 1 {
		return errors.New("知らない引数: " + fs.Arg(1))
	}
	c, err := NewCounter(*limit)
	if err != nil {
		return err
	}
	ops, err := ParseOps(fs.Arg(0))
	if err != nil {
		return err
	}
	c.Apply(ops)
	_, err = fmt.Fprintln(out, c.Value())
	return err
}
