package session

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/go-sql-driver/mysql"
	"github.com/iancoleman/strcase"
	"github.com/jmoiron/sqlx"
	"go.temporal.io/server/common/auth"
	"go.temporal.io/server/common/config"
	"go.temporal.io/server/common/persistence/sql/sqlplugin"
	"go.temporal.io/server/common/resolver"
)

type Session struct {
	*sqlx.DB
}

const (
	driverName = "mysql"

	isolationLevelAttrName       = "transaction_isolation"
	isolationLevelAttrNameLegacy = "tx_isolation"
	defaultIsolationLevel        = "'READ-COMMITTED'"
	// customTLSName is the name used if a custom tls configuration is created
	customTLSName = "tls-custom"

	interpolateParamsAttr = "interpolateParams"
)

var (
	errVisInterpolateParamsNotSupported = errors.New("interpolateParams is not supported for mysql visibility stores")
	dsnAttrOverrides                    = map[string]string{
		"parseTime":       "true",
		"clientFoundRows": "true",
	}
)

func NewSession(
	dbKind sqlplugin.DbKind,
	cfg *config.SQL,
	resolver resolver.ServiceResolver,
) (*Session, error) {
	db, err := createConnection(dbKind, cfg, resolver)
	if err != nil {
		return nil, err
	}
	return &Session{DB: db}, nil
}

func (s *Session) Close() {
	if s.DB != nil {
		_ = s.DB.Close()
	}
}

func createConnection(
	dbKind sqlplugin.DbKind,
	cfg *config.SQL,
	resolver resolver.ServiceResolver,
) (*sqlx.DB, error) {
	err := registerTLSConfig(cfg)
	if err != nil {
		return nil, err
	}

	dsn, err := buildDSN(dbKind, cfg, resolver)
	if err != nil {
		return nil, err
	}

	db, err := sqlx.Connect(driverName, dsn)
	if err != nil {
		return nil, err
	}
	if cfg.MaxConns > 0 {
		db.SetMaxOpenConns(cfg.MaxConns)
	}
	if cfg.MaxIdleConns > 0 {
		db.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	if cfg.MaxConnLifetime > 0 {
		db.SetConnMaxLifetime(cfg.MaxConnLifetime)
	}

	// Maps struct names in CamelCase to snake without need for db struct tags.
	db.MapperFunc(strcase.ToSnake)
	return db, nil
}

func buildDSN(
	dbKind sqlplugin.DbKind,
	cfg *config.SQL,
	r resolver.ServiceResolver,
) (string, error) {
	mysqlConfig := mysql.NewConfig()

	mysqlConfig.User = cfg.User
	mysqlConfig.Passwd = cfg.Password
	mysqlConfig.Addr = r.Resolve(cfg.ConnectAddr)[0]
	mysqlConfig.DBName = cfg.DatabaseName
	mysqlConfig.Net = cfg.ConnectProtocol
	var err error
	mysqlConfig.Params, err = buildDSNAttrs(dbKind, cfg)
	if err != nil {
		return "", err
	}

	// https://github.com/go-sql-driver/mysql/blob/v1.5.0/dsn.go#L104-L106
	// https://github.com/go-sql-driver/mysql/blob/v1.5.0/dsn.go#L182-L189
	if mysqlConfig.Net == "" {
		mysqlConfig.Net = "tcp"
	}

	// https://github.com/go-sql-driver/mysql#rejectreadonly
	// https://github.com/temporalio/temporal/issues/1703
	mysqlConfig.RejectReadOnly = true

	return mysqlConfig.FormatDSN(), nil
}

func paramInterpolationAllowed(dbKind sqlplugin.DbKind) bool {
	return dbKind != sqlplugin.DbKindVisibility
}

func buildDSNAttrs(dbKind sqlplugin.DbKind, cfg *config.SQL) (map[string]string, error) {
	attrs := make(map[string]string, len(dsnAttrOverrides)+len(cfg.ConnectAttributes)+1)
	// Enable interpolation by default unless this is a mysql8 visibility store
	if paramInterpolationAllowed(dbKind) {
		attrs[interpolateParamsAttr] = "true"
	}
	for k, v := range cfg.ConnectAttributes {
		k1, v1 := sanitizeAttr(k, v)
		attrs[k1] = v1
	}

	// only override isolation level if not specified
	if !hasAttr(attrs, isolationLevelAttrName) &&
		!hasAttr(attrs, isolationLevelAttrNameLegacy) {
		attrs[isolationLevelAttrName] = defaultIsolationLevel
	}

	// these attrs are always overriden
	for k, v := range dsnAttrOverrides {
		attrs[k] = v
	}

	if !paramInterpolationAllowed(dbKind) {
		if _, ok := attrs[interpolateParamsAttr]; ok {
			return nil, errVisInterpolateParamsNotSupported
		}
	}

	return attrs, nil
}

func hasAttr(attrs map[string]string, key string) bool {
	_, ok := attrs[key]
	return ok
}

func sanitizeAttr(inkey string, invalue string) (string, string) {
	key := strings.ToLower(strings.TrimSpace(inkey))
	value := strings.ToLower(strings.TrimSpace(invalue))
	switch key {
	case isolationLevelAttrName, isolationLevelAttrNameLegacy:
		if value[0] != '\'' { // mysql sys variable values must be enclosed in single quotes
			value = "'" + value + "'"
		}
		return key, value
	default:
		return inkey, invalue
	}
}

func registerTLSConfig(cfg *config.SQL) error {
	if cfg.TLS == nil || !cfg.TLS.Enabled {
		return nil
	}

	// TODO: create a way to set MinVersion and CipherSuites via cfg.
	tlsConfig := auth.NewTLSConfigForServer(cfg.TLS.ServerName, cfg.TLS.EnableHostVerification)

	if cfg.TLS.CaFile != "" {
		rootCertPool := x509.NewCertPool()
		pem, err := os.ReadFile(cfg.TLS.CaFile)
		if err != nil {
			return fmt.Errorf("failed to load CA files: %v", err)
		}
		if ok := rootCertPool.AppendCertsFromPEM(pem); !ok {
			return fmt.Errorf("failed to append CA file")
		}
		tlsConfig.RootCAs = rootCertPool
	}

	if cfg.TLS.CertFile != "" && cfg.TLS.KeyFile != "" {
		clientCert := make([]tls.Certificate, 0, 1)
		certs, err := tls.LoadX509KeyPair(
			cfg.TLS.CertFile,
			cfg.TLS.KeyFile,
		)
		if err != nil {
			return fmt.Errorf("failed to load tls x509 key pair: %v", err)
		}
		clientCert = append(clientCert, certs)
		tlsConfig.Certificates = clientCert
	}

	// In order to use the TLS configuration you need to register it. Once registered you use it by specifying
	// `tls` in the connect attributes.
	err := mysql.RegisterTLSConfig(customTLSName, tlsConfig)
	if err != nil {
		return fmt.Errorf("failed to register tls config: %v", err)
	}

	if cfg.ConnectAttributes == nil {
		cfg.ConnectAttributes = map[string]string{}
	}

	// If no `tls` connect attribute is provided then we override it to our newly registered tls config automatically.
	// This allows users to simply provide a tls config without needing to remember to also set the connect attribute
	if cfg.ConnectAttributes["tls"] == "" {
		cfg.ConnectAttributes["tls"] = customTLSName
	}

	return nil
}
