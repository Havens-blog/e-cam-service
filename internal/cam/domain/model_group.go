package domain

// ModelGroup 模型分组
type ModelGroup struct {
	ID    int64  `json:"id" bson:"id"`
	Name  string `json:"name" bson:"name"`
	Ctime int64  `json:"ctime" bson:"ctime"`
	Utime int64  `json:"utime" bson:"utime"`
}
