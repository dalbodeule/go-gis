#include <QColor>
#include <QFont>
#include <QJSValue>
#include <QPointer>
#include <QQuickItem>
#include <QQuickTextDocument>
#include <QSet>
#include <QSyntaxHighlighter>
#include <QTextCharFormat>
#include <QTextDocument>
#include <QTimer>
#include <QVariant>
#include <QtQml/qqml.h>

namespace {

class LuaSyntaxHighlighter final : public QSyntaxHighlighter {
public:
    explicit LuaSyntaxHighlighter(QTextDocument* document) : QSyntaxHighlighter(document) {}

protected:
    void highlightBlock(const QString& line) override {
        // A positive block state means a long Lua string is open; a negative
        // state means a long comment is open. The magnitude records '=' count.
        int index = 0;
        int state = previousBlockState();
        if (state != -1 && state != 0) {
            const bool comment = state < -1;
            const int equals = comment ? -state - 2 : state - 1;
            const QString closing = QStringLiteral("]") + QString(equals, QLatin1Char('=')) + QLatin1Char(']');
            const int close = line.indexOf(closing);
            if (close < 0) {
                setFormat(0, line.size(), comment ? commentFormat() : stringFormat());
                setCurrentBlockState(state);
                return;
            }
            index = close + closing.size();
            setFormat(0, index, comment ? commentFormat() : stringFormat());
        }
        setCurrentBlockState(0);
        while (index < line.size()) {
            const int start = index;
            const QChar ch = line.at(index);
            if (ch == QLatin1Char('-') && index + 1 < line.size() && line.at(index + 1) == QLatin1Char('-')) {
                const int opening = longBracketEquals(line, index + 2);
                if (opening >= 0) {
                    const int body = index + 4 + opening;
                    const QString closing = QStringLiteral("]") + QString(opening, QLatin1Char('=')) + QLatin1Char(']');
                    const int close = line.indexOf(closing, body);
                    index = close < 0 ? line.size() : close + closing.size();
                    setFormat(start, index - start, commentFormat());
                    if (close < 0) setCurrentBlockState(-opening - 2);
                } else {
                    setFormat(start, line.size() - start, commentFormat());
                    return;
                }
            } else if (ch == QLatin1Char('[') && longBracketEquals(line, index) >= 0) {
                const int opening = longBracketEquals(line, index);
                const int body = index + 2 + opening;
                const QString closing = QStringLiteral("]") + QString(opening, QLatin1Char('=')) + QLatin1Char(']');
                const int close = line.indexOf(closing, body);
                index = close < 0 ? line.size() : close + closing.size();
                setFormat(start, index - start, stringFormat());
                if (close < 0) setCurrentBlockState(opening + 1);
            } else if (ch == QLatin1Char('"') || ch == QLatin1Char('\'')) {
                const QChar quote = ch;
                ++index;
                while (index < line.size()) {
                    if (line.at(index) == QLatin1Char('\\')) {
                        index = qMin(index + 2, line.size());
                    } else if (line.at(index++) == quote) {
                        break;
                    }
                }
                setFormat(start, index - start, stringFormat());
            } else if (ch.isLetter() || ch == QLatin1Char('_')) {
                ++index;
                while (index < line.size() && (line.at(index).isLetterOrNumber() || line.at(index) == QLatin1Char('_'))) ++index;
                const QString identifier = line.mid(start, index - start);
                if (keywords().contains(identifier)) setFormat(start, index - start, keywordFormat());
                else if (apis().contains(identifier)) setFormat(start, index - start, apiFormat());
            } else if (ch.isDigit()) {
                ++index;
                while (index < line.size() && (line.at(index).isDigit() || line.at(index) == QLatin1Char('.'))) ++index;
                setFormat(start, index - start, numberFormat());
            } else {
                ++index;
            }
        }
    }

private:
    static int longBracketEquals(const QString& line, int start) {
        if (start >= line.size() || line.at(start) != QLatin1Char('[')) return -1;
        int index = start + 1;
        while (index < line.size() && line.at(index) == QLatin1Char('=')) ++index;
        return index < line.size() && line.at(index) == QLatin1Char('[') ? index - start - 1 : -1;
    }
    static const QSet<QString>& keywords() {
        static const QSet<QString> values = {"and", "break", "do", "else", "elseif", "end", "false", "for", "function", "if", "in", "local", "nil", "not", "or", "repeat", "return", "then", "true", "until", "while"};
        return values;
    }
    static const QSet<QString>& apis() {
        static const QSet<QString> values = {"feature", "gogis", "math", "string", "table", "ipairs", "pairs", "next", "tonumber", "tostring", "type"};
        return values;
    }
    static QTextCharFormat colorFormat(const char* color, bool bold = false, bool italic = false) {
        QTextCharFormat format;
        format.setForeground(QColor(QString::fromLatin1(color)));
        if (bold) format.setFontWeight(QFont::DemiBold);
        if (italic) format.setFontItalic(true);
        return format;
    }
    static const QTextCharFormat& keywordFormat() { static const auto value = colorFormat("#7b2cbf", true); return value; }
    static const QTextCharFormat& apiFormat() { static const auto value = colorFormat("#1769aa"); return value; }
    static const QTextCharFormat& stringFormat() { static const auto value = colorFormat("#16803c"); return value; }
    static const QTextCharFormat& numberFormat() { static const auto value = colorFormat("#b45309"); return value; }
    static const QTextCharFormat& commentFormat() { static const auto value = colorFormat("#78838e", false, true); return value; }
};

class GoGISLuaHighlighter : public QQuickItem {
public:
    explicit GoGISLuaHighlighter(QQuickItem* parent = nullptr) : QQuickItem(parent) {
        timer_.setInterval(100);
        QObject::connect(&timer_, &QTimer::timeout, this, [this]() { attachDocument(); });
        timer_.start();
    }

private:
    void attachDocument() {
        const QVariant value = property("textDocument");
        QObject* object = value.value<QObject*>();
        if (!object && value.canConvert<QJSValue>()) object = value.value<QJSValue>().toQObject();
        auto* wrapper = qobject_cast<QQuickTextDocument*>(object);
        QTextDocument* document = wrapper ? wrapper->textDocument() : nullptr;
        if (document == document_) return;
        if (highlighter_) delete highlighter_;
        document_ = document;
        if (document) {
            highlighter_ = new LuaSyntaxHighlighter(document);
            timer_.stop();
        }
    }
    QTimer timer_;
    QPointer<QTextDocument> document_;
    QPointer<LuaSyntaxHighlighter> highlighter_;
};

} // namespace

void gogis_register_lua_highlighter_type() {
    qmlRegisterType<GoGISLuaHighlighter>("GoGIS", 1, 0, "LuaHighlighter");
}
