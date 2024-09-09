package main

import (
	"context"
    "go.uber.org/zap"
	"goEasyDocker/internal"
    "flag"
    "log"
    "github.com/docker/docker/client"
    "github.com/docker/docker/api/types"
)

// care after changing oImageName or oImageTag may loose some caches ones
func createBaseImage(goWorkAddr string, oModAddr string, oImageRepo string, oImageTag string) {
	lg, _ := zap.NewDevelopment()
	goWorkParser, err := tools.GoWorkspaceParser{}.New(goWorkAddr, lg)
	if err != nil {
		lg.Fatal("failed to parse go.work", zap.Error(err))
	}
	
    goMerger, err := tools.GoModMerger{}.New(oModAddr, goWorkParser, lg)
	if err != nil {
		lg.Fatal("failed to merge modfiles", zap.Error(err))
	}
    
    err = goMerger.AddNewModFile()
	if err != nil {
		lg.Fatal("failed to create new modfile", zap.Error(err))
	}
    
    goImgWorker, err := tools.GoImageWorker{}.New(goMerger, lg, context.Background(), client.WithAPIVersionNegotiation(), client.WithHostFromEnv())
	if err != nil {
		lg.Fatal("failed to init imageworker", zap.Error(err))
	}
    
    resp, err := goImgWorker.BuildModsImage(types.ImageBuildOptions {CacheFrom: []string{oImageRepo}})
    if err != nil {
        lg.Fatal("failed to build mods final image", zap.Error(err))
    }
    
    imageId, err := goImgWorker.ReadImageBuildResponse(resp)
    if err != nil {
        lg.Fatal("failed to parse build response", zap.Error(err))
    }
    lg.Info("Successfully created base image", zap.String("id", imageId), zap.String("repository", oImageRepo), zap.String("tag",  oImageTag))

    err = goImgWorker.TagModsImage(imageId, oImageRepo, oImageTag)
    if err != nil { 
        lg.Fatal("failed to tag image", zap.Error(err))
    }
}

func main() {
    var workerAddr, outputAddr, outputImageName, outputImageTag string
    flag.StringVar(&workerAddr, "f", "", "worker file dir address")
    flag.StringVar(&outputAddr, "d", ".", "output dir with modfiles addr")
    flag.StringVar(&outputImageName, "o", "goeasydockerimage", "output image name")
    flag.StringVar(&outputImageTag, "t", "latest", "output image tag")
    flag.Parse()
    if workerAddr == "" {
        log.Fatal("got unexpected default work file addr")
    }
    createBaseImage(workerAddr, outputAddr, outputImageName, outputImageTag)
}
