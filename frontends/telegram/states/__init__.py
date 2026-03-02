"""State management package for the Notes Bot."""

from .shared import state_manager
from .context import UserState, UserContext
from .manager import StateManager

__all__ = ["state_manager", "UserState", "UserContext", "StateManager"]
