package gormv2

import "gorm.io/gorm"

// DBConditions DB常用的查询条件封装
type DBConditions struct {
	And       map[string]any
	Or        map[string]any
	Not       map[string]any
	Limit     int
	Offset    int
	Order     any
	Select    any
	Group     string
	Having    any
	NeedCount bool
	Count     int64
	Distinct  any
	DoUpdates map[string]any
}

// Fill 填充查询条件
func (d *DBConditions) Fill(db *gorm.DB) *gorm.DB {
	if d.Select != nil {
		db = db.Select(d.Select)
	}

	for cond, val := range d.And {
		db = db.Where(cond, val)
	}
	for cond, val := range d.Not {
		db = db.Not(cond, val)
	}
	for cond, val := range d.Or {
		db = db.Or(cond, val)
	}

	if d.NeedCount {
		db = db.Count(&d.Count)
	}
	if d.Order != nil {
		db = db.Order(d.Order)
	}
	if d.Limit != 0 {
		db = db.Limit(d.Limit)
	}
	if d.Offset != 0 {
		db = db.Offset(d.Offset)
	}
	if d.Group != "" {
		db = db.Group(d.Group)
	}
	if d.Having != nil {
		db = db.Having(d.Having)
	}
	if d.Distinct != nil {
		db = db.Distinct(d.Distinct)
	}

	return db
}

/* demo
cond := &base.DBConditions{
	And: map[string][]any{
		"id IN (?)": {95,96,97},
	},
	Not: map[string][]any{
		"id": {96},
	},
	Limit: 1,
	Offset: 1,
	Order: "id DESC",
}
*/
