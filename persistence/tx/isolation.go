package tx

import "database/sql"

type IsolationLevel int

const (
    IsolationDefault IsolationLevel = iota
    IsolationReadUncommitted
    IsolationReadCommitted
    IsolationWriteCommitted
    IsolationRepeatableRead
    IsolationSnapshot
    IsolationSerializable
    IsolationLinearizable
)

func (l IsolationLevel) SQL() (sql.IsolationLevel, error) {
    switch l {
    case IsolationDefault:
        return sql.LevelDefault, nil
    case IsolationReadUncommitted:
        return sql.LevelReadUncommitted, nil
    case IsolationReadCommitted:
        return sql.LevelReadCommitted, nil
    case IsolationWriteCommitted:
        return sql.LevelWriteCommitted, nil
    case IsolationRepeatableRead:
        return sql.LevelRepeatableRead, nil
    case IsolationSnapshot:
        return sql.LevelSnapshot, nil
    case IsolationSerializable:
        return sql.LevelSerializable, nil
    case IsolationLinearizable:
        return sql.LevelLinearizable, nil
    default:
        return 0, ErrUnsupportedLevel
    }
}
