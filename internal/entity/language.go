package entity

// Language 语言查询及更新
type Language struct {
	ID       int    `json:"id" binding:"omitempty,number"`
	Name     string `json:"name"`
	ActionId int    `json:"action_id"`
	Status   *bool  `json:"status" binding:"omitempty"`
	Page     int    `json:"page" binding:"omitempty,number"`
	PageSize int    `json:"page_size" binding:"omitempty,number"`
}

// LanguageSync 测评语言同步接收结构
type LanguageSync struct {
	Id       string `json:"Id" binding:"omitempty,number"`
	Name     string `json:"Name" binding:"omitempty"`
	Template string `json:"Template" binding:"omitempty"`
}

/*
 {
        "CompileCmd": "",
        "Connections": null,
        "ContainerOptions": {
            "CompileTTL": 30,
            "MemoryLimit": 104857600,
            "RunTTL": 5
        },
        "DefaultFiles": null,
        "EnableExternalCommands": "all",
        "Enabled": false,
        "Groups": [
            "bash"
        ],
        "Id": "default",
        "IsDefault": true,
        "IsSupportPackage": false,
        "Name": "Version",
        "Provider": "built-in",
        "RunCmd": "bash -v",
        "ScriptOptions": {
            "SourceFile": ""
        },
        "Template": "bash",
        "Version": "1.0",
        "Workdir": "/app"
    }
*/
