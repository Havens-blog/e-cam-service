package domain

const (
	MappingOneToOne   = "1:1" // 一对一关系
	MappingOneToMany  = "1:n" // 一对多关系
	MappingManyToMany = "n:n" // 多对多关系
)

// RelationType 关系类型
type RelationType struct {
	ID             int64  `json:"id" bson:"id"`
	Name           string `json:"name" bson:"name"`
	UID            string `json:"uid" bson:"uid"`
	SourceDescribe string `json:"source_describe" bson:"source_describe"`
	TargetDescribe string `json:"target_describe" bson:"target_describe"`
	Ctime          int64  `json:"ctime" bson:"ctime"`
	Utime          int64  `json:"utime" bson:"utime"`
}

// ModelRelation 模型关系
type ModelRelation struct {
	ID              int64  `json:"id" bson:"id"`
	SourceModelUID  string `json:"source_model_uid" bson:"source_model_uid"`
	TargetModelUID  string `json:"target_model_uid" bson:"target_model_uid"`
	RelationTypeUID string `json:"relation_type_uid" bson:"relation_type_uid"`
	RelationName    string `json:"relation_name" bson:"relation_name"`
	Mapping         string `json:"mapping" bson:"mapping"`
	Ctime           int64  `json:"ctime" bson:"ctime"`
	Utime           int64  `json:"utime" bson:"utime"`
}
