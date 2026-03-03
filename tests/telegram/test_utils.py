"""Tests for frontends/telegram/utils.py (pure logic, no async)."""

from frontends.telegram.utils import escape_markdown_v2


class TestEscapeMarkdownV2:
    def test_plain_text_passes_through(self):
        assert escape_markdown_v2("hello world") == "hello world"

    def test_empty_string(self):
        assert escape_markdown_v2("") == ""

    def test_escapes_underscore(self):
        assert escape_markdown_v2("_") == r"\_"

    def test_escapes_asterisk(self):
        assert escape_markdown_v2("*") == r"\*"

    def test_escapes_square_brackets(self):
        assert escape_markdown_v2("[") == r"\["
        assert escape_markdown_v2("]") == r"\]"

    def test_escapes_parentheses(self):
        assert escape_markdown_v2("(") == r"\("
        assert escape_markdown_v2(")") == r"\)"

    def test_escapes_tilde(self):
        assert escape_markdown_v2("~") == r"\~"

    def test_escapes_backtick(self):
        assert escape_markdown_v2("`") == r"\`"

    def test_escapes_greater_than(self):
        assert escape_markdown_v2(">") == r"\>"

    def test_escapes_hash(self):
        assert escape_markdown_v2("#") == r"\#"

    def test_escapes_plus(self):
        assert escape_markdown_v2("+") == r"\+"

    def test_escapes_minus(self):
        assert escape_markdown_v2("-") == r"\-"

    def test_escapes_equals(self):
        assert escape_markdown_v2("=") == r"\="

    def test_escapes_pipe(self):
        assert escape_markdown_v2("|") == r"\|"

    def test_escapes_curly_braces(self):
        assert escape_markdown_v2("{") == r"\{"
        assert escape_markdown_v2("}") == r"\}"

    def test_escapes_dot(self):
        assert escape_markdown_v2(".") == r"\."

    def test_escapes_exclamation(self):
        assert escape_markdown_v2("!") == r"\!"

    def test_escapes_date_format(self):
        result = escape_markdown_v2("04-Mar-2026")
        assert result == r"04\-Mar\-2026"

    def test_escapes_multiple_special_chars_in_text(self):
        result = escape_markdown_v2("Hello (world)!")
        assert result == r"Hello \(world\)\!"

    def test_numbers_not_escaped(self):
        assert escape_markdown_v2("1234567890") == "1234567890"

    def test_letters_not_escaped(self):
        assert escape_markdown_v2("abcABC") == "abcABC"

    def test_mixed_content(self):
        result = escape_markdown_v2("rating: 7/10!")
        assert result == r"rating: 7/10\!"
