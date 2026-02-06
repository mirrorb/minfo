package main

import (
    "context"
    "errors"
    "fmt"
    "io/fs"
    "os"
    "path/filepath"
    "strings"
)

var errNoISO = errors.New("no iso found")
var errISOFound = errors.New("iso found")

func resolveScreenshotSource(ctx context.Context, input string) (string, func(), error) {
    info, err := os.Stat(input)
    if err != nil {
        return "", noop, err
    }

    if !info.IsDir() {
        if isISOFile(input) {
            return resolveM2TSFromMountedISO(ctx, input)
        }
        return input, noop, nil
    }

    if bdmvRoot, ok := resolveBDMVRoot(input); ok {
        m2ts, err := findLargestM2TS(bdmvRoot)
        if err != nil {
            return "", noop, err
        }
        return m2ts, noop, nil
    }

    isoPath, err := findISOInDir(input)
    if err == nil {
        return resolveM2TSFromMountedISO(ctx, isoPath)
    }
    if !errors.Is(err, errNoISO) {
        return "", noop, err
    }

    return "", noop, errors.New("path does not contain BDMV or BDISO content")
}

func resolveBDInfoSource(ctx context.Context, input string) (string, func(), error) {
    info, err := os.Stat(input)
    if err != nil {
        return "", noop, err
    }

    if !info.IsDir() {
        if isISOFile(input) {
            return resolveBDInfoFromMountedISO(ctx, input)
        }
        return input, noop, nil
    }

    if bdmvRoot, ok := resolveBDInfoRoot(input); ok {
        return bdmvRoot, noop, nil
    }

    isoPath, err := findISOInDir(input)
    if err == nil {
        return resolveBDInfoFromMountedISO(ctx, isoPath)
    }
    if !errors.Is(err, errNoISO) {
        return "", noop, err
    }

    return "", noop, errors.New("path does not contain BDMV or BDISO content")
}

func resolveBDInfoRoot(path string) (string, bool) {
    base := filepath.Base(path)
    if strings.EqualFold(base, "BDMV") {
        return path, true
    }
    if strings.EqualFold(base, "STREAM") {
        return filepath.Dir(path), true
    }
    bdmv := filepath.Join(path, "BDMV")
    if info, err := os.Stat(bdmv); err == nil && info.IsDir() {
        return bdmv, true
    }
    return "", false
}

func resolveBDMVRoot(path string) (string, bool) {
    base := filepath.Base(path)
    if strings.EqualFold(base, "BDMV") {
        return path, true
    }
    if strings.EqualFold(base, "STREAM") {
        return path, true
    }
    bdmv := filepath.Join(path, "BDMV")
    if info, err := os.Stat(bdmv); err == nil && info.IsDir() {
        return bdmv, true
    }
    return "", false
}

func isISOFile(path string) bool {
    return strings.EqualFold(filepath.Ext(path), ".iso")
}

func findISOInDir(root string) (string, error) {
    var isoPath string
    err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
        if err != nil {
            return err
        }
        if d.IsDir() {
            return nil
        }
        if isISOFile(path) {
            isoPath = path
            return errISOFound
        }
        return nil
    })
    if err != nil && !errors.Is(err, errISOFound) {
        return "", err
    }
    if isoPath == "" {
        return "", errNoISO
    }
    return isoPath, nil
}

func findLargestM2TS(root string) (string, error) {
    searchRoot := root
    stream := filepath.Join(root, "STREAM")
    if info, err := os.Stat(stream); err == nil && info.IsDir() {
        searchRoot = stream
    }

    var largestPath string
    var largestSize int64

    err := filepath.WalkDir(searchRoot, func(path string, d fs.DirEntry, err error) error {
        if err != nil {
            return err
        }
        if d.IsDir() {
            return nil
        }
        if !strings.EqualFold(filepath.Ext(path), ".m2ts") {
            return nil
        }
        info, err := d.Info()
        if err != nil {
            return err
        }
        if info.Size() > largestSize {
            largestSize = info.Size()
            largestPath = path
        }
        return nil
    })
    if err != nil {
        return "", err
    }
    if largestPath == "" {
        return "", errors.New("no m2ts files found under BDMV")
    }
    return largestPath, nil
}

func resolveM2TSFromMountedISO(ctx context.Context, isoPath string) (string, func(), error) {
    mountDir, cleanup, err := mountISO(ctx, isoPath)
    if err != nil {
        return "", noop, err
    }

    bdmvRoot, ok := resolveBDMVRoot(mountDir)
    if !ok {
        cleanup()
        return "", noop, errors.New("BDMV folder not found in ISO")
    }

    m2ts, err := findLargestM2TS(bdmvRoot)
    if err != nil {
        cleanup()
        return "", noop, err
    }

    return m2ts, cleanup, nil
}

func resolveBDInfoFromMountedISO(ctx context.Context, isoPath string) (string, func(), error) {
    mountDir, cleanup, err := mountISO(ctx, isoPath)
    if err != nil {
        return "", noop, err
    }

    if _, ok := resolveBDInfoRoot(mountDir); !ok {
        cleanup()
        return "", noop, errors.New("BDMV folder not found in ISO")
    }

    return mountDir, cleanup, nil
}

func mountISO(ctx context.Context, isoPath string) (string, func(), error) {
    mountBin, err := resolveBin("MOUNT_BIN", "mount")
    if err != nil {
        return "", noop, err
    }
    umountBin, err := resolveBin("UMOUNT_BIN", "umount")
    if err != nil {
        return "", noop, err
    }

    mountDir, err := os.MkdirTemp("", "minfo-iso-mount-*")
    if err != nil {
        return "", noop, err
    }

    mountCtx, cancel := context.WithTimeout(ctx, mountTimeout)
    defer cancel()

    _, stderr, err := runCommand(mountCtx, mountBin, "-o", "loop,ro", isoPath, mountDir)
    if err != nil {
        _ = os.RemoveAll(mountDir)
        msg := strings.TrimSpace(stderr)
        if msg == "" {
            msg = err.Error()
        }
        return "", noop, fmt.Errorf("mount iso failed: %s", msg)
    }

    cleanup := func() {
        umountCtx, cancel := context.WithTimeout(context.Background(), umountTimeout)
        defer cancel()
        if _, _, err := runCommand(umountCtx, umountBin, mountDir); err != nil {
            _, _, _ = runCommand(umountCtx, umountBin, "-l", mountDir)
        }
        _ = os.RemoveAll(mountDir)
    }

    return mountDir, cleanup, nil
}
