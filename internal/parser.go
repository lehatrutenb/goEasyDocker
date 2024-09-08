package tools

import (
    "golang.org/x/mod/modfile"
    "os"
    "go.uber.org/zap"
)

type GoWorkspaceParser struct {
    addr string
    wf *modfile.WorkFile
    mfs []*modfile.File
    mfsBData [][]byte
    lg *zap.Logger
}

func (wp *GoWorkspaceParser) initWorkFile() error {
    fBData, err := os.ReadFile(wp.addr + "/go.work")
    if err != nil {
        wp.lg.Error("failed to read go.work", zap.Error(err))
        return err
    }

    wp.wf, err = modfile.ParseWork(wp.addr + "/go.work", fBData, nil)
    if err != nil {
        wp.lg.Error("failed to parse go.work", zap.Error(err))
    }
    return nil
}

// add struct later to params
func (wp *GoWorkspaceParser) parseModFile(module *modfile.Use, path string) (*modfile.File, []byte, error) {
    if module == nil {
        wp.lg.Warn("got nil module")
        return nil, nil, nil
    }
    wp.lg.Debug("Trying to parse .mod file", zap.String("complete path", path))
    fBData, err := os.ReadFile(path)
    if err != nil {
        wp.lg.Error("failed to read modfile", zap.Error(err), zap.String("path", path))
        return nil, nil, err
    }
    
    mf, err := modfile.Parse(module.Path, fBData, nil)
    if err != nil {
        wp.lg.Error("failed to parse go.work", zap.Error(err))
        return nil, nil, err
    }
    wp.lg.Debug("parsed modfile", zap.Any("modfile", mf))
    return mf, fBData, nil
}

func (wp *GoWorkspaceParser) initModFiles() error {
    //wp.mfs = make([]*modfile.File, 0) why there? mv to New
    if wp.mfs == nil {
        wp.lg.Warn("workfile is nil")
        return nil
    }
    for _, module := range(wp.wf.Use) {
        if module == nil {
            continue
        }
        mf, bData, err := wp.parseModFile(module, wp.addr + "/" + module.Path + "/go.mod")
        if err != nil {
            return err
        }
        wp.mfs = append(wp.mfs, mf)
        wp.mfsBData = append(wp.mfsBData, bData)
    }
    return nil
}

func (_ GoWorkspaceParser) New(addr string, lg *zap.Logger) (*GoWorkspaceParser, error) {
    wp := &GoWorkspaceParser{lg:lg, addr:addr, mfs: make([]*modfile.File, 0), mfsBData: make([][]byte, 0)}
    err := wp.initWorkFile()
    if err != nil {
        return nil, err
    }
    err = wp.initModFiles()
    if err != nil {
        return nil, err
    }
    return wp, nil
}

