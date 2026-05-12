"""Admin HTTP API for the moderation service — review queue + human override."""
import json
import logging
import time
import uuid

from fastapi import FastAPI, HTTPException, Request, Response, Header
from pydantic import BaseModel
from typing import Optional

import db as dbmod

logger = logging.getLogger("moderation.api")

app = FastAPI(docs_url=None, redoc_url=None)


def _require_admin(x_user_role: str = Header(default="")) -> None:
    if x_user_role.lower() != "admin":
        raise HTTPException(status_code=403, detail="admin role required")


class RejectBody(BaseModel):
    reason: Optional[str] = ""


@app.get("/api/moderation/queue")
def get_queue(
    market: str = "",
    status: str = "auto_rejected",
    x_user_role: str = Header(default=""),
):
    _require_admin(x_user_role)

    params: list = []
    where = []
    if market:
        params.append(market)
        where.append(f"market = ${len(params)}")
    if status:
        params.append(status)
        where.append(f"status = ${len(params)}")
    else:
        params.append("auto_rejected")
        where.append(f"status = ${len(params)}")

    sql = f"""
        SELECT id, article_id, market, COALESCE(topic, '') AS topic,
               status, quality_score, cultural_ok, factual_ok,
               rejection_reasons, created_at
        FROM moderation_svc.review_queue
        {"WHERE " + " AND ".join(where) if where else ""}
        ORDER BY created_at DESC
        LIMIT 100
    """
    rows = dbmod.fetchall(sql, tuple(params))

    result = []
    for r in rows:
        result.append({
            "id": str(r["id"]),
            "article_id": str(r["article_id"]),
            "market": r["market"],
            "topic": r["topic"],
            "status": r["status"],
            "score": float(r["quality_score"] or 0),
            "cultural_ok": r["cultural_ok"],
            "factual_ok": r["factual_ok"],
            "rejection_reasons": list(r["rejection_reasons"] or []),
            "created_at": r["created_at"].isoformat() if hasattr(r["created_at"], "isoformat") else str(r["created_at"]),
        })
    return result


@app.post("/api/moderation/approve/{item_id}")
def approve_item(
    item_id: str,
    request: Request,
    x_user_role: str = Header(default=""),
    x_user_id: str = Header(default=""),
):
    _require_admin(x_user_role)

    row = dbmod.fetchone(
        "SELECT article_id, market FROM moderation_svc.review_queue WHERE id = %s",
        (item_id,),
    )
    if not row:
        raise HTTPException(status_code=404, detail="item not found")

    dbmod.execute(
        """UPDATE moderation_svc.review_queue
           SET status = 'human_approved', reviewed_at = now(),
               reviewed_by = %s::uuid
           WHERE id = %s""",
        (x_user_id or None, item_id),
    )

    # Emit article.approved via Kafka producer stored in app state
    producer = getattr(request.app.state, "producer", None)
    if producer:
        approved = {
            "event_id": str(uuid.uuid4()),
            "article_id": str(row["article_id"]),
            "market": row["market"],
            "moderator_id": x_user_id or "admin-override",
            "quality_score": 1.0,
            "timestamp": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        }
        producer.produce(
            "article.approved",
            key=str(row["article_id"]).encode(),
            value=json.dumps(approved).encode(),
        )
        producer.poll(0)
        logger.info("human-approved article via admin: %s", row["article_id"])

    return {"approved": True}


@app.post("/api/moderation/reject/{item_id}")
def reject_item(
    item_id: str,
    body: RejectBody,
    x_user_role: str = Header(default=""),
    x_user_id: str = Header(default=""),
):
    _require_admin(x_user_role)

    row = dbmod.fetchone(
        "SELECT id FROM moderation_svc.review_queue WHERE id = %s",
        (item_id,),
    )
    if not row:
        raise HTTPException(status_code=404, detail="item not found")

    dbmod.execute(
        """UPDATE moderation_svc.review_queue
           SET status = 'human_rejected',
               reviewed_at = now(),
               reviewed_by = %s::uuid,
               rejection_reasons = array_append(rejection_reasons, %s)
           WHERE id = %s""",
        (x_user_id or None, body.reason or "editor override", item_id),
    )
    return {"rejected": True}
