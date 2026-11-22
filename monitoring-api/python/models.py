import uuid
from datetime import datetime, timezone
from enum import Enum
from pydantic import BaseModel, Field, HttpUrl


class MonitorStatus(str, Enum):
    UNKNOWN = "UNKNOWN"
    UP = "UP"
    DOWN = "DOWN"


class MonitorCreateRequest(BaseModel):
    name: str
    url: HttpUrl
    interval_seconds: int = Field(ge=5)
    timeout_ms: int = Field(ge=100)
    failure_threshold: int = Field(ge=1)


class Monitor(BaseModel):
    id: str
    name: str
    url: str
    interval_seconds: int
    timeout_ms: int
    failure_threshold: int
    status: MonitorStatus = MonitorStatus.UNKNOWN
    consecutive_failures: int = 0
    created_at: datetime
    updated_at: datetime

    @classmethod
    def create(cls, req: MonitorCreateRequest) -> "Monitor":
        now = datetime.now(timezone.utc)
        return cls(
            id=str(uuid.uuid4()),
            name=req.name,
            url=str(req.url),
            interval_seconds=req.interval_seconds,
            timeout_ms=req.timeout_ms,
            failure_threshold=req.failure_threshold,
            created_at=now,
            updated_at=now,
        )

    # TODO: record_result(success: bool) -> status transition logic per Section 4
