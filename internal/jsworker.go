package tools

import (
    "encoding/json"    
    "fmt"
    //"reflect"
)

const MaxReqLevel = 10

type JsData struct {
    d map[string]JsData
    s string
}

func parseObjReq(data map[string]any) map[string]JsData {
    res := make(map[string]JsData)
    for key, val := range(data) {
        switch val.(type) {
        case string:
            res[key] = JsData{s: val.(string), d: nil}
        case map[string]any:
            res[key] = JsData{s: "", d: parseObjReq(val.(map[string]any))}
        default:
            res[key] = JsData{s: fmt.Sprint(val), d: nil}
        }
    }
    return res
}

func parseJsReqToMap(data []byte) (map[string]JsData, error) {
    pData := make(map[string]any)
    if err := json.Unmarshal(data, &pData); err != nil {
        return nil, err
    }
    return parseObjReq(pData), nil
}
