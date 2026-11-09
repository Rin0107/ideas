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

// Run は引数を受け取って結果を out に書く。
func Run(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("{{NAME}}", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	showVersion := fs.Bool("version", false, "バージョンを表示する")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("引数: %w", err)
	}
	if *showVersion {
		_, err := fmt.Fprintln(out, Version)
		return err
	}
	if fs.NArg() > 0 {
		return errors.New("知らない引数: " + fs.Arg(0))
	}
	_, err := fmt.Fprintln(out, "{{GOAL}}")
	return err
}
