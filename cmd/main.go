package main

import (
    //"golang.org/x/tools/go/packages"
    "golang.org/x/mod/modfile"
    "golang.org/x/mod/module"
    "fmt"
    "os"
    //"io"
    "context"
    "errors"
    //"strings"
    "go.uber.org/zap"
    "strconv"
    "github.com/docker/docker/client"
    "bytes"
    "github.com/docker/docker/api/types"
    "archive/tar"
    //"github.com/docker/docker/api/types/image"
)

type goWorkspaceParser struct {
    addr string
    wf *modfile.WorkFile
    mfs []*modfile.File
    mfsBData [][]byte
    lg *zap.Logger
}

func (wp *goWorkspaceParser) initWorkFile() error {
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

func (wp *goWorkspaceParser) parseModFile(module *modfile.Use) (*modfile.File, []byte, error) {
    if module == nil {
        wp.lg.Warn("got nil module")
        return nil, nil, nil
    }
    fBData, err := os.ReadFile(wp.addr + "/" + module.Path + "/go.mod")
    if err != nil {
        wp.lg.Error("failed to read modfile", zap.Error(err), zap.String("path", module.Path))
        return nil, nil, err
    }
    
    mf, err := modfile.Parse(module.Path, fBData, nil)
    if err != nil {
        wp.lg.Error("failed to parse go.work", zap.Error(err))
        return nil, nil, err
    }
    return mf, fBData, nil
}

func (wp *goWorkspaceParser) initModFiles() error {
    //wp.mfs = make([]*modfile.File, 0) why there? mv to New
    if wp.mfs == nil {
        wp.lg.Warn("workfile is nil")
        return nil
    }
    for _, module := range(wp.wf.Use) {
        if module == nil {
            continue
        }
        mf, bData, err := wp.parseModFile(module)
        if err != nil {
            return err
        }
        wp.mfs = append(wp.mfs, mf)
        wp.mfsBData = append(wp.mfsBData, bData)
    }
    return nil
}

func (_ goWorkspaceParser) New(addr string, lg *zap.Logger) (*goWorkspaceParser, error) {
    wp := &goWorkspaceParser{lg:lg, addr:addr, mfs: make([]*modfile.File, 0), mfsBData: make([][]byte, 0)}
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

type goModMerger struct {
    oModPath string
    wp *goWorkspaceParser
    curMfs []*modfile.File
    curMfsBData [][]byte
    curReqs map[string]string
    lg *zap.Logger
}

func (_ goModMerger) New(outputModPath string, wp *goWorkspaceParser, lg *zap.Logger) (*goModMerger, error) {
    mm := &goModMerger {oModPath: outputModPath + "/mods/", wp: wp, curMfs: make([]*modfile.File, 0), curMfsBData: make([][]byte, 0), curReqs: make(map[string]string), lg: lg}
    if err := mm.loadPrev(); err != nil {
        return nil, err
    }
    return mm, nil
}

func (mm *goModMerger) addReqs(mf *modfile.File) (map[string]string, error) {
    if mf == nil {
        mm.lg.Warn("got nil modfile")
        return make(map[string]string), nil
    }
    if mf.Require == nil {
        mm.lg.Warn("got nil reqs")
        return make(map[string]string), nil
    }

    reqs := make(map[string]string)
    for _, req := range(mf.Require) {
        if req == nil {
            continue
        }

        escPath, err := module.EscapePath(req.Mod.Path)
        if err != nil {
            mm.lg.Error("failed to escape req path")
            return nil, err
        }
        escVersion, err := module.EscapeVersion(req.Mod.Version)
        if err != nil {
            mm.lg.Error("failed to escape req version")
            return nil, err
        }
        /*if curVersion, ok := mm.curReqs[escPath]; ok && curVersion != escVersion {
            mm.lg.Warn("find different versions of module", zap.String("first version", curVersion), zap.String("second version", escVersion), zap.String("module", mf.Module.Mod.Path))
        }*/
        reqs[escPath] = escVersion
    }
    return reqs, nil
}

func (mm *goModMerger) mergeModReqs(mfs []*modfile.File) (map[string]string, error) {
    reqs := make(map[string]string)
    for _, mf := range(mfs) {
        if mf == nil {
            continue
        }
        curReqs, err := mm.addReqs(mf)
        if err != nil {
            return nil, err
        }
        for path, version := range(curReqs) {
            if curVersion, ok := reqs[path]; ok && curVersion != version {
                mm.lg.Warn("find different versions of module", zap.String("first version", curVersion), zap.String("second version", version), zap.String("module", mf.Module.Mod.Path))
            }
            reqs[path] = version
        }
    }
    return reqs, nil
}

func (mm *goModMerger) loadPrev() error {
    modules, err := os.ReadDir(mm.oModPath)
    if err != nil {
        mm.lg.Warn("failed to read prev merged modfiles", zap.Error(err))
        if !errors.Is(err, os.ErrNotExist) {
            return err
        }
    }
    if errors.Is(err, os.ErrNotExist) {
        if err = os.MkdirAll(mm.oModPath, os.ModePerm); err != nil {
            mm.lg.Error("failed to create dir for modfiles", zap.Error(err))
            return err
        }
    }

    for _, module := range(modules) {
        mf, bData, err := mm.wp.parseModFile(&modfile.Use{Path: mm.oModPath + module.Name()})
        if err != nil {
            return err
        }
        if mf == nil {
            continue
        }
        mm.curMfs = append(mm.curMfs, mf)
        mm.curMfsBData = append(mm.curMfsBData, bData)
    }
    
    mm.curReqs, err = mm.mergeModReqs(mm.curMfs)
    if err != nil {
        return err
    }
    return nil
}

func (mm *goModMerger) updateCurMfs(mf *modfile.File, bData []byte) {
    mm.curMfs = append(mm.curMfs, mf)
    mm.curMfsBData = append(mm.curMfsBData, bData)
    for _, req := range(mf.Require) {
          if req == nil {
              continue
          }
          mm.curReqs[req.Mod.Path] = req.Mod.Version
    }
}

func (mm *goModMerger) addNewModFile() error {
    if len(mm.wp.mfs) == 0 {
        mm.lg.Info("no new reqs added")
        return nil
    }
    reqs, err := mm.mergeModReqs(mm.wp.mfs)
    if err != nil {
        return err
    }
    newMf := mm.wp.mfs[0] // questionable solution not to init new modfile by myself
    //newMf.Module.Mod.Path = "go" + strconv.Itoa(len(mm.curMfs)) + ".mod" // update module name to just filename
    newMf.Syntax.Name = "go" + strconv.Itoa(len(mm.curMfs)) + ".mod"
    for path, version := range(reqs) {
        if curVersion, ok := mm.curReqs[path]; ok || curVersion == version {
            continue
        }
        newMf.AddRequire(path, version)
    }

    bData, err := newMf.Format()
    if err != nil {
        mm.lg.Error("failed to fromat new modfile", zap.Error(err))
        return err
    }
    if err = os.WriteFile(mm.oModPath + newMf.Syntax.Name, bData, os.ModePerm); err != nil {
        mm.lg.Error("failed to write new modfile", zap.Error(err))
        return err
    }
    mm.updateCurMfs(newMf, bData)
    return nil
}

type goImageWorker struct {
    cli *client.Client
    mm *goModMerger
    lg *zap.Logger
    ctx context.Context
}

func (_ goImageWorker) New(mm *goModMerger, lg *zap.Logger, ctx context.Context) (*goImageWorker, error) {
    cli, err := client.NewClientWithOpts(client.WithAPIVersionNegotiation())
	if err != nil {
        lg.Error("failed to init docker client", zap.Error(err))
		return nil, err
	}
    return &goImageWorker{mm: mm, lg: lg, cli: cli, ctx: ctx}, nil
}

// i haven't found smth that implemets dockerfile editing from box - so as i need just to create nearly simular files i added small implementation
func (iw *goImageWorker) generateDockerfile() []byte {
    sData := "FROM golang:latest AS baseModfile\n" // parent image
    for _, mf := range(iw.mm.curMfs) { // download all modfiles one by one
        sData += fmt.Sprintf("COPY %v go.mod\n RUN go mod download\n", mf.Module.Mod.Path) // add path as in curMfs path is just a file's name
    }
    return []byte(sData)
}

// put dockerfile, current modfiles in tar
func (iw *goImageWorker) generateImageBuilderBody() *bytes.Buffer {
    buf := bytes.NewBuffer([]byte{})
    tarWriter := tar.NewWriter(buf)

    // dockerfile
    dfBData := iw.generateDockerfile()
    tarWriter.WriteHeader(&tar.Header{Name: "Dockerfile", Mode: int64(os.ModePerm), Size:  int64(len(dfBData))})
    tarWriter.Write(dfBData)

    //mods
    for i, mfBData := range(iw.mm.curMfsBData) {
        tarWriter.WriteHeader(&tar.Header{Name: iw.mm.curMfs[i].Syntax.Name, Mode: int64(os.ModePerm), Size: int64(len(mfBData))})
        tarWriter.Write(mfBData)
    }

    tarWriter.Close()
    return buf
}

// care, buildModsImage wants to create .goEasyDockerDockerfile and than rm it
func (iw *goImageWorker) buildModsImage(imageName string) (types.ImageBuildResponse, error) {
    /* #so i'm happy now#
    if err := os.WriteFile(".goEasyDockerDockerfile", iw.generateDockerfile(), os.ModePerm); err != nil { // i really will be happy to to create such files
        iw.mm.lg.Error("failed to write dockerfile", zap.Error(err))
        return err
    }*/

    resp, err := iw.cli.ImageBuild(iw.ctx, iw.generateImageBuilderBody(), types.ImageBuildOptions {Tags: []string{imageName}})
    if err != nil {
        iw.mm.lg.Error("failed to build image", zap.Error(err));
        return types.ImageBuildResponse{}, err
    }
    /*if err := os.Remove(".goEasyDockerfile"); err != nil {
        iw.mm.lg.Error("failed to remove temp dockerfile", zap.Error(err))
        return err
    }*/
    return resp, nil
}

func main() {
    lg, _ := zap.NewDevelopment()
    goWorkAddr := "./test"
    goWorkParser, err := goWorkspaceParser{}.New(goWorkAddr, lg)
    if err != nil {
        lg.Fatal("", zap.Error(err))
    }
    fmt.Println(goWorkParser, err)
    goMerger, err := goModMerger{}.New(".", goWorkParser, lg)
    if err != nil {
        lg.Fatal("", zap.Error(err)) // E: string literal not terminated
    }
    err = goMerger.addNewModFile()
    if err != nil {
        lg.Fatal("", zap.Error(err)) // E: string literal not terminated
    }
    goImgWorker, err := goImageWorker{}.New(goMerger, lg, context.Background())
    if err != nil {
        lg.Fatal("", zap.Error(err))
    }
    fmt.Println(goImgWorker.buildModsImage("testimage"))
}
