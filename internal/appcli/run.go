// Package appcli implements local administration; no setup endpoint is exposed.
package appcli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
	"portexa/internal/accounts"
	"portexa/internal/auth"
	"portexa/internal/httpapi"
	"portexa/internal/storage"
	"portexa/internal/syncer"
	"portexa/internal/vault"
)

func Run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("用法: portexa init|serve|password-reset|backup|restore [选项]，使用 -h 查看")
	}
	command := args[0]
	if command != "init" && command != "serve" && command != "password-reset" && command != "backup" && command != "restore" {
		return errors.New("未知命令")
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	dataDir := flags.String("data", ".local/portexa", "私有运行目录")
	keyPath := flags.String("key-file", "", "独立凭据密钥文件，默认 DATA/keys/credentials.key")
	passwordStdin := flags.Bool("password-stdin", false, "从标准输入读取一行系统密码，仅用于本地初始化或重置")
	listen := flags.String("listen", "127.0.0.1:8080", "HTTP 监听地址，生产置于 HTTPS 反向代理后")
	origin := flags.String("origin", "", "唯一浏览器来源，例如 https://portexa.example.com")
	dev := flags.Bool("dev-http", false, "仅允许环回 HTTP 开发访问")
	webDir := flags.String("web", ".local/web", "构建后的前端目录")
	backupPath := flags.String("file", "", "备份输出文件，或恢复输入文件")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("不接受位置参数")
	}
	abs, err := filepath.Abs(*dataDir)
	if err != nil {
		return err
	}
	if *keyPath == "" {
		*keyPath = filepath.Join(abs, "keys", "credentials.key")
	}
	unlock, err := lock(abs)
	if err != nil {
		return err
	}
	defer unlock()
	dbPath := filepath.Join(abs, "db", "portexa.sqlite")
	if command == "restore" {
		if *backupPath == "" {
			return errors.New("请通过 -file 指定备份，-data 指向新的空运行目录")
		}
		source, err := filepath.Abs(*backupPath)
		if err != nil {
			return err
		}
		if err = storage.RestoreNew(ctx, source, dbPath); err != nil {
			return err
		}
		fmt.Println("已验证并恢复数据库；会话已撤销，采集已暂停。请恢复匹配的独立密钥，核对接入点后手动启用账户。")
		return nil
	}
	if command != "init" {
		if _, err := os.Stat(dbPath); err != nil {
			return errors.New("尚未初始化，请先运行 portexa init")
		}
	}
	store, err := storage.Open(dbPath)
	if err != nil {
		return err
	}
	defer store.Close()
	authService := auth.New(store, auth.Options{})
	switch command {
	case "init":
		if _, err := store.Auth(ctx); !errors.Is(err, storage.ErrNotFound) {
			return errors.New("系统已经初始化或无法读取，拒绝覆盖")
		}
		password, err := readPassword(*passwordStdin)
		if err != nil {
			return err
		}
		if _, err = vault.Load(*keyPath); errors.Is(err, os.ErrNotExist) {
			_, err = vault.Create(*keyPath)
			if err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if err = authService.Initialize(ctx, password); err != nil {
			return err
		}
		fmt.Println("已初始化单用户登录和独立凭据密钥。请将密钥单独备份，不要与数据库打包。")
		return nil
	case "password-reset":
		password, err := readPassword(*passwordStdin)
		if err != nil {
			return err
		}
		if err = authService.ResetPassword(ctx, password); err != nil {
			return err
		}
		fmt.Println("密码已重置，全部会话已撤销。")
		return nil
	case "backup":
		if *backupPath == "" {
			return errors.New("请通过 -file 指定新的备份文件")
		}
		if err = store.Backup(ctx, *backupPath); err != nil {
			return err
		}
		fmt.Println("数据库备份已通过完整性及外键验证；不包含凭据密钥。")
		return nil
	case "serve":
		if _, err = store.Auth(ctx); err != nil {
			return errors.New("尚未设置系统密码，请先初始化")
		}
		if *dev {
			host, _, err := net.SplitHostPort(*listen)
			if err != nil || (host != "127.0.0.1" && host != "::1" && host != "localhost") {
				return errors.New("开发模式只能监听环回地址")
			}
			if *origin == "" {
				*origin = "http://" + *listen
			}
		}
		if _, err := os.Stat(filepath.Join(*webDir, "index.html")); err != nil {
			return errors.New("前端未构建，请先执行 make web-build")
		}
		key, err := vault.Load(*keyPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "凭据密钥不可用：保留已有数据，账户采集将停止；请恢复匹配密钥。")
		}
		service := accounts.New(store, key, nil)
		engine := syncer.New(service, nil, syncer.Options{})
		handler, err := httpapi.New(authService, service, engine, httpapi.Config{Origin: *origin, DevelopmentHTTP: *dev, Static: os.DirFS(*webDir)})
		if err != nil {
			return err
		}
		return serve(ctx, *listen, handler, store, engine, filepath.Join(abs, "backups"))
	}
	return nil
}

func readPassword(stdin bool) (string, error) {
	if stdin {
		reader := bufio.NewReader(io.LimitReader(os.Stdin, 4097))
		password, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return "", err
		}
		password = strings.TrimSuffix(strings.TrimSuffix(password, "\n"), "\r")
		return password, auth.ValidatePassword(password)
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", errors.New("请在终端输入密码，或使用 -password-stdin 安全管道")
	}
	fmt.Fprint(os.Stderr, "设置系统密码（至少16个字符）：")
	first, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	fmt.Fprint(os.Stderr, "再次输入：")
	second, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	if string(first) != string(second) {
		return "", errors.New("两次密码不一致")
	}
	return string(first), auth.ValidatePassword(string(first))
}

func lock(dataDir string) (func(), error) {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(dataDir, ".portexa.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		file.Close()
		return nil, errors.New("运行目录已被占用，请停止主服务后执行本地管理命令")
	}
	return func() { _ = unix.Flock(int(file.Fd()), unix.LOCK_UN); _ = file.Close() }, nil
}

func serve(parent context.Context, address string, handler http.Handler, store *storage.Store, engine *syncer.Engine, backupDir string) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	server := &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	defer wg.Wait()
	defer cancel()
	errorsCh := make(chan error, 2)
	wg.Add(2)
	go func() { defer wg.Done(); errorsCh <- engine.Run(ctx) }()
	go func() {
		defer wg.Done()
		store.Maintain(ctx, backupDir, func(error) { fmt.Fprintln(os.Stderr, "数据库维护未完成，请检查空间和备份目录。") })
	}()
	go func() { errorsCh <- server.Serve(listener) }()
	fmt.Println("Portexa 已启动：", address, "SQLite", store.Version())
	select {
	case <-ctx.Done():
	case err = <-errorsCh:
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
	}
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	return errors.Join(err, server.Shutdown(shutdownCtx))
}
