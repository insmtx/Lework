package store

import (
	"fmt"

	"gorm.io/gorm"
)

// legacyNodeNameIndex 是升级前 nodes.name 上的唯一索引名。
//
// 它由旧模型的 `uniqueIndex` 标签生成、名字与 GORM 的默认命名一致，因此升级后
// 的新模型必须把展示名字段放到另一个索引名下（idx_nodes_name_lookup），
// 否则「先建新索引再删旧索引」会连新索引一起删掉。
const legacyNodeNameIndex = "idx_nodes_name"

// migrateControlPlane 建表并清理 AutoMigrate 做不到的 DDL 变更。
//
// GORM 的 AutoMigrate 只增列、增索引，从不删除或重建已有索引；nodes.name 从
// 唯一索引改成普通索引不会被它识别，旧库会因此继续拒绝同名节点。这里显式
// 删除旧唯一索引，SQLite（单元测试）与 PostgreSQL（生产）走同一段代码：
// Migrator 的 HasIndex/DropIndex 在两种方言下都会翻译成当前方言的 DDL。
func migrateControlPlane(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("migrate control plane: nil database")
	}

	// NodeMember 排在 Node 之后：它的语义（按 node_id 归属）依赖 nodes 已存在，
	// 让日志里的迁移错误按依赖顺序出现更容易定位。NodeClaim 同样引用未来的
	// node_id，排在归属表之后。
	models := []any{
		&Node{}, &NodeMember{}, &Device{}, &ProjectIndex{}, &ProjectPreference{}, &Preference{},
		&User{}, &UserSession{}, &Invite{}, &NodeClaim{},
	}
	if err := db.AutoMigrate(models...); err != nil {
		return fmt.Errorf("auto migrate control plane tables: %w", err)
	}

	migrator := db.Migrator()
	if migrator.HasIndex(&Node{}, legacyNodeNameIndex) {
		if err := migrator.DropIndex(&Node{}, legacyNodeNameIndex); err != nil {
			return fmt.Errorf("drop legacy index %q: %w", legacyNodeNameIndex, err)
		}
	}
	return nil
}
