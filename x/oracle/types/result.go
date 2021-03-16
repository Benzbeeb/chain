package types

// NewResult creates a new Result instance.
func NewResult(req OracleRequestPacketData, res OracleResponsePacketData) Result {
	return Result{
		RequestPacketData:  req,
		ResponsePacketData: res,
	}
}
