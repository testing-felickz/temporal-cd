package masker

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMaskStruct(t *testing.T) {
	assert := assert.New(t)

	type strct struct {
		Login    string
		Password string
	}

	s1 := strct{
		Login:    "login",
		Password: "password",
	}
	maskedS1 := MaskStruct(s1, DefaultFieldNames)
	assert.Equal("password", s1.Password)
	assert.Equal("******", maskedS1.(*strct).Password)
	assert.Equal("login", maskedS1.(*strct).Login)

	s2 := &strct{
		Login:    "login",
		Password: "password",
	}
	maskedS2 := MaskStruct(s2, DefaultFieldNames)
	assert.Equal("password", s2.Password)
	assert.Equal("******", maskedS2.(*strct).Password)
	assert.Equal("login", maskedS2.(*strct).Login)
}

func TestMaskStruct_Nil(t *testing.T) {
	assert := assert.New(t)

	maskedS1 := MaskStruct(nil, DefaultFieldNames)
	assert.Nil(maskedS1)

	var nilInterface interface{}
	maskedS2 := MaskStruct(nilInterface, DefaultFieldNames)
	assert.Nil(maskedS2)

	var nilInt *int
	maskedS3 := MaskStruct(nilInt, DefaultFieldNames)
	assert.Nil(maskedS3)
}

func TestMaskYaml(t *testing.T) {
	assert := assert.New(t)
	yaml := `persistence:
  defaultStore: mysql-default
  visibilityStore: mysql-visibility
  numHistoryShards: 4
  datastores:
    mysql-default:
      sql:
        pluginName: "mysql"
        databaseName: "temporal"
        connectAddr: "127.0.0.1:3306"
        connectProtocol: "tcp"
        user: "temporal"
        password: "secret"`

	maskedYaml, err := MaskYaml(yaml, DefaultYAMLFieldNames)
	assert.NoError(err)
	assert.True(strings.Contains(yaml, "secret"))
	assert.False(strings.Contains(maskedYaml, "secret"))
	assert.True(strings.Contains(maskedYaml, "******"))

	fmt.Println(maskedYaml)
}
