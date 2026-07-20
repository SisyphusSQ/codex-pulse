package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

var (
	// ErrProtoMapping 表示内部 DTO 无法安全映射到冻结的 Protobuf contract。
	ErrProtoMapping = errors.New("core protobuf mapping failed")
	numericType     = reflect.TypeFor[basequery.NumericValue]()
)

// EncodeResponse 将现有强类型 DTO 映射到同形的 Protobuf response。
//
// 映射前递归验证 NumericValue，避免把 value/unknown 的非法组合送到跨进程 surface。
func EncodeResponse(source any, target proto.Message) error {
	if source == nil || target == nil {
		return ErrProtoMapping
	}
	if err := validateNumericValues(reflect.ValueOf(source)); err != nil {
		return fmt.Errorf("%w: validate numeric presence: %w", ErrProtoMapping, err)
	}
	content, err := json.Marshal(source)
	if err != nil {
		return fmt.Errorf("%w: marshal internal dto: %w", ErrProtoMapping, err)
	}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(content, target); err != nil {
		return fmt.Errorf("%w: unmarshal protobuf dto: %w", ErrProtoMapping, err)
	}
	return nil
}

func validateNumericValues(value reflect.Value) error {
	if !value.IsValid() {
		return nil
	}
	for value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil
		}
		value = value.Elem()
	}
	if value.Type() == numericType {
		numeric, ok := value.Interface().(basequery.NumericValue)
		if !ok {
			return ErrProtoMapping
		}
		return numeric.Validate()
	}
	switch value.Kind() {
	case reflect.Struct:
		for i := range value.NumField() {
			field := value.Field(i)
			if !field.CanInterface() {
				continue
			}
			if err := validateNumericValues(field); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		for i := range value.Len() {
			if err := validateNumericValues(value.Index(i)); err != nil {
				return err
			}
		}
	case reflect.Map:
		iterator := value.MapRange()
		for iterator.Next() {
			if err := validateNumericValues(iterator.Value()); err != nil {
				return err
			}
		}
	}
	return nil
}
