SELECT action, skipped_rounds FROM english_review_history WHERE round_no > 0 AND token = $1;
