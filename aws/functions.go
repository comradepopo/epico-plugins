package main

import (
    "bytes"
    "time"
    "io/ioutil"
    "net/http"
    "strings"

    "../../epico/signers/aws_v4"
    utils "../../epico/utils"
    generic_structs "../../epico/structs"
    xj "github.com/basgys/goxml2json"
)

var PluginAuthFunction = PluginAuth
var PluginPostProcessFunction = PluginPostProcess
var PluginPagingPeekFunction = PluginPagingPeek


// authParams expects the AWS ACCESS ID in slice slot [0] and the AWS SECRET KEY
//     in slice slot [1].
func PluginAuth( apiRequest generic_structs.ApiRequest, authParams []string ) []byte {

    var err error
    signer := v4.NewSigner( generic_structs.ApiCredentials{
            Id: authParams[0],
            Key: authParams[1],
        }, func(v4Signer *v4.Signer) {
            v4Signer.DisableHeaderHoisting = false
            v4Signer.DisableRequestBodyOverwrite = true

    })

    apiRequest.Time = time.Now()
    if apiRequest.Settings.Vars["region"] != "{{region}}" {
        apiRequest.FullRequest.Header, err = signer.Presign( apiRequest.FullRequest, nil, apiRequest.Settings.Vars["service"], apiRequest.Settings.Vars["region"], 60 * time.Minute, time.Now() )
    } else {
        apiRequest.FullRequest.Header, err = signer.Presign( apiRequest.FullRequest, nil, apiRequest.Settings.Vars["service"], "us-east-1", 60 * time.Minute, time.Now() )
    }

    if err != nil {
        utils.LogFatal("AWS:PluginAuth", "Error presigning the AWS request", err)
        return nil
    }


    client := &http.Client{}
    resp, err := client.Do(apiRequest.FullRequest)
    if err != nil {
        utils.LogFatal("AWS:PluginAuth", "Error running the AWS request", err)
        return nil
    }
    defer resp.Body.Close()
    // TODO: Handle failed connections better / handle retry?
    // i/o timeoutpanic: runtime error: invalid memory address or nil pointer dereference
    // [signal SIGSEGV: segmentation violation code=0x1 addr=0x40 pc=0x6aa2ba]

    body, err := ioutil.ReadAll(resp.Body)
    if err != nil {
        utils.LogFatal("AWS:PluginAuth", "Error reading AWS request body", err)
        return nil
    }

    return body
}


func PluginPagingPeek( response []byte, responseKeys []string, oldPageValue interface{} ) ( interface{}, bool ) {

    responseMap := utils.XmlResponseProcess( response )

    var pageValue interface{}
    for _, v := range responseKeys {
        if pageValue == nil {
            pageValue = responseMap[v]
        } else {
            pageValue = pageValue.(map[string]interface{})[v]
        }
    }

    if pageValue == oldPageValue {
        pageValue = nil
    }
    return pageValue, ( pageValue != "" && pageValue != nil )
}


func PluginPostProcess( apiResponseMap map[generic_structs.ComparableApiRequest][]byte, jsonKeys []map[string]string ) []byte {

    parsedStructure := make(map[string]interface{})
    parsedErrorStructure := make(map[string]interface{})

    for response, apiResponse := range apiResponseMap {
        jsonBody, err := xj.Convert( bytes.NewReader( apiResponse ) )
        if err != nil {
            utils.LogFatal("AWS:PluginPostProcess", "Error converting XML API response", err)
            return nil
        }

        // This chunk reads in the list of responses and handles AWS' terrible
        //    XML return with infinite "item"/"member" tags.
        processedJson := jsonBody.Bytes()
        for _, v := range jsonKeys {
            if  v["api_call_name"] == response.Name {
                xmlKeys := strings.Split( v["xml_tags"], "," )
                for _, k := range xmlKeys {
                    processedJson = utils.RemoveXmlTagFromJson(
                        k, processedJson)
                }
            }
        }

        utils.ParsePostProcessedJson( response, processedJson, parsedStructure, parsedErrorStructure )

    }

    returnJson := utils.CollapseJson( parsedStructure, parsedErrorStructure )
    return returnJson
}
