package tools

import (
    "fmt"
    "os"
    "context"
    "go.uber.org/zap"
    "github.com/docker/docker/client"
    "github.com/docker/docker/api/types/image"
    "bytes"
    "strings"
    "github.com/docker/docker/api/types"
    "archive/tar"
    "errors"
    "io"
)


type GoImageWorker struct {
    cli *client.Client
    mm *GoModMerger
    lg *zap.Logger
    ctx context.Context
}

func (_ GoImageWorker) New(mm *GoModMerger, lg *zap.Logger, ctx context.Context, clOpts ...client.Opt) (*GoImageWorker, error) {
    cli, err := client.NewClientWithOpts(clOpts...)
	if err != nil {
        lg.Error("failed to init docker client", zap.Error(err))
		return nil, err
	}
    return &GoImageWorker{mm: mm, lg: lg, cli: cli, ctx: ctx}, nil
}

// i haven't found smth that implemets dockerfile editing from box - so as i need just to create nearly simular files i added small implementation
func (iw *GoImageWorker) generateDockerfile() []byte {
    sData := "FROM golang:latest AS baseModfile\n" // parent image
    for _, mf := range(iw.mm.curMfs) { // download all modfiles one by one
        sData += fmt.Sprintf("COPY %v go.mod\nRUN go mod download\n", mf.Syntax.Name) // add path as in curMfs path is just a file's name
    }
    iw.lg.Debug("dockefile data", zap.String("text", sData))
    return []byte(sData)
}

// put dockerfile, current modfiles in tar
func (iw *GoImageWorker) generateImageBuilderBody() *bytes.Buffer {
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
func (iw *GoImageWorker) BuildModsImage(imgBuildOpts types.ImageBuildOptions) (types.ImageBuildResponse, error) {
    resp, err := iw.cli.ImageBuild(iw.ctx, iw.generateImageBuilderBody(), imgBuildOpts)
    if err != nil {
        iw.mm.lg.Error("failed to build image", zap.Error(err));
        return types.ImageBuildResponse{}, err
    }
    return resp, nil
}

var ErrNoImageIdGot error = errors.New("failed to parse image id from docker engine responses")

// !!! TODO: rework parser response to strings
func (iw *GoImageWorker) ReadImageBuildResponse(resp types.ImageBuildResponse) (imageId string, err error) {
    buf := make([]byte, 10000)
    allBuf := make([]byte, 0)
    for {
        n, err := resp.Body.Read(buf)
        allBuf = append(allBuf, buf[:n]...)
        if errors.Is(err, io.EOF) {
            break
        }
    }

    spBuf := strings.Split(string(allBuf), "}\r\n{") // reason to rework
    for i, line := range(spBuf) {
        // as rmed during splitting
        {
            if i != 0 {
                line = "{" + line
            }
            if i != len(spBuf) - 1 {
                line = line + "}"
            }
        }
        resp, errParse := parseJsReqToMap([]byte(line))
        if errParse != nil {
            iw.mm.lg.Error("failed to unmarshall data", zap.Error(errParse), zap.String("text", string(line)))
            return "", errParse
        }
        if aux, ok := resp["aux"]; ok && aux.d != nil {
            if id, ok := aux.d["ID"]; ok && id.s != "" {
                imageId = id.s
            }
        }
    }
    if imageId == "" {
        return "", ErrNoImageIdGot
    }
    return imageId, nil
}

// source can be image name or id
func (iw *GoImageWorker) TagModsImage(name string, repo string, tag string) (error) {
    err := iw.cli.ImageTag(iw.ctx, name, repo+":"+tag)
    if err != nil {
        iw.mm.lg.Error("failed to tag an image", zap.Error(err), zap.String("image name", name), zap.String("new repo", repo), zap.String("new tag", tag))
        return err
    }
    return nil
}

// TODO add repo tag too (if we want some versioning)
// to rm prevs images
func (iw *GoImageWorker) RemoveModsImage(name string) (error) { // img name or id
    _, err := iw.cli.ImageRemove(iw.ctx, name, image.RemoveOptions{}) // TODO work with remove response
    if err != nil {
        iw.mm.lg.Error("failed to remove an image", zap.Error(err), zap.String("image name", name))
        return err
    }
    return nil   
}
