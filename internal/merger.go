package tools

import (
    "golang.org/x/mod/modfile"
    "golang.org/x/mod/module"
    "os"
    "errors"
    "go.uber.org/zap"
    "strconv"
)   


type GoModMerger struct {
    oModPath string
    wp *GoWorkspaceParser
    curMfs []*modfile.File
    curMfsBData [][]byte
    curReqs map[string]string
    lg *zap.Logger
}

func (_ GoModMerger) New(outputModPath string, wp *GoWorkspaceParser, lg *zap.Logger) (*GoModMerger, error) {
    mm := &GoModMerger {oModPath: outputModPath + "/mods/", wp: wp, curMfs: make([]*modfile.File, 0), curMfsBData: make([][]byte, 0), curReqs: make(map[string]string), lg: lg}
    if err := mm.loadPrev(); err != nil {
        return nil, err
    }
    return mm, nil
}

func (mm *GoModMerger) addReqs(mf *modfile.File) (map[string]string, error) {
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

        // am I really need escaped version - as also I will have to do unascape after it
        reqs[req.Mod.Path] = req.Mod.Version
    }
    return reqs, nil
}

func (mm *GoModMerger) mergeModReqs(mfs []*modfile.File) (map[string]string, error) {
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

func (mm *GoModMerger) loadPrev() error {
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

    for _, curModule := range(modules) {
        tmpMf := modfile.Use{Path: mm.oModPath + curModule.Name()}
        mf, bData, err := mm.wp.parseModFile(&tmpMf, tmpMf.Path)
        if err != nil {
            return err
        }
        if mf == nil {
            continue
        }
        mf.Module = &modfile.Module{Mod: module.Version{}}
        mf.Module.Mod.Path = tmpMf.Path
        mm.curMfs = append(mm.curMfs, mf)
        mm.curMfsBData = append(mm.curMfsBData, bData)
    }
    
    mm.curReqs, err = mm.mergeModReqs(mm.curMfs)
    if err != nil {
        return err
    }
    return nil
}

func (mm *GoModMerger) updateCurMfs(mf *modfile.File, bData []byte) {
    mm.curMfs = append(mm.curMfs, mf)
    mm.curMfsBData = append(mm.curMfsBData, bData)
    for _, req := range(mf.Require) {
          if req == nil {
              continue
          }
          mm.curReqs[req.Mod.Path] = req.Mod.Version
    }
}

func (mm *GoModMerger) AddNewModFile() error {
    if len(mm.wp.mfs) == 0 {
        mm.lg.Warn("no modfiles found")
        return nil
    }
    reqs, err := mm.mergeModReqs(mm.wp.mfs)
    if err != nil {
        mm.lg.Error("failed to merge reqs", zap.Error(err))
        return err
    }
    
    var newMf *modfile.File = &modfile.File{}
    newMf.AddModuleStmt("GoEasyDockerModfile")
    newMf.Syntax.Name = "go" + strconv.Itoa(len(mm.curMfs)) + ".mod"
    reqAdded := false
    
    for path, version := range(reqs) {
        if curVersion, ok := mm.curReqs[path]; ok && curVersion == version {
            continue
        }
        reqAdded = true
        newMf.AddRequire(path, version)
    }

    if !reqAdded {
        mm.lg.Debug("no new reqs found")
        return nil
    }

    bData, err := newMf.Format()
    if err != nil {
        mm.lg.Error("failed to format new modfile", zap.Error(err))
        return err
    }
    if len(bData) == 0 { // modfile is empty <=> nop new reqs 
        mm.lg.Info("no new reqs added")
        return nil
    }
    if err = os.WriteFile(mm.oModPath + newMf.Syntax.Name, bData, os.ModePerm); err != nil {
        mm.lg.Error("failed to write new modfile", zap.Error(err))
        return err
    }
    mm.updateCurMfs(newMf, bData)
    return nil
}
