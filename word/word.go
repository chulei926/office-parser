package word

import (
	"bytes"
	"fmt"
	"gitee.com/zhexiao/unioffice/common"
	"gitee.com/zhexiao/unioffice/document"
	"github.com/zhexiao/mtef-go/eqn"
	"github.com/zhexiao/office-parser/utils"
	"io"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"
)

type RowData struct {
	Content     []string
	HtmlContent []string
}

type TableData struct {
	Rows []*RowData
}

type Word struct {
	Uri    string
	Tables []*TableData
	doc    *document.Document

	//公式对象 RID:LATEX 的对应关系
	oles map[string]*string
	//图片 RID:七牛地址 的对应关系
	images map[string]string
}

func NewWord() *Word {
	return &Word{}
}

//直接读文件内容
func Read(r io.ReaderAt, size int64) *Word {
	doc, err := document.Read(r, size)
	if err != nil {
		log.Panic(err)
	}

	return parser(doc)
}

//打开文件内容
func Open(filepath string) *Word {
	doc, err := document.Open(filepath)
	if err != nil {
		log.Panic(err)
	}

	return parser(doc)
}

//解析word
func parser(doc *document.Document) *Word {
	//得到doc指针数据
	w := NewWord()
	w.doc = doc
	w.parseOle(w.doc.OleObjectPaths)
	w.parseImage(w.doc.Images)

	//todo 得到文档的所有公式一次性解析
	//fmt.Println(w.doc.OleObjectWmfPath)

	//读取table数据
	w.getTableData()

	return w
}

//读取表单数据
func (w *Word) getTableData() {
	tables := w.doc.Tables()
	for _, table := range tables {
		//读取一个表单里面的所有行
		rows := table.Rows()

		//读取行里面的数据
		tableData := w.getRowsData(&rows)
		w.Tables = append(w.Tables, &tableData)
	}
}

//读取所有行的数据
func (w *Word) getRowsData(rows *[]document.Row) TableData {
	var td TableData
	for _, row := range *rows {
		rowData := w.getRowText(&row)
		td.Rows = append(td.Rows, &rowData)
	}

	return td
}

//读取每一行的数据
func (w *Word) getRowText(row *document.Row) RowData {
	cells := row.Cells()
	rowData := RowData{}

	for _, cell := range cells {
		rawText, htmlText := w.getCellText(&cell)
		rowData.Content = append(rowData.Content, rawText)
		rowData.HtmlContent = append(rowData.HtmlContent, htmlText)
	}

	return rowData
}

//读取行里面每一个单元的数据
func (w *Word) getCellText(cell *document.Cell) (string, string) {
	paragraphs := cell.Paragraphs()

	resText := bytes.Buffer{}
	htmlResText := bytes.Buffer{}

	//循环每一个P标签数据
	for paragIdx, paragraph := range paragraphs {
		runs := paragraph.Runs()

		for _, r := range runs {
			var text string

			//图片数据
			if r.DrawingInline() != nil {
				for _, di := range r.DrawingInline() {
					imf, _ := di.GetImage()
					uri := w.images[imf.RelID()]

					text = fmt.Sprintf("<img src='%s' style='width:%s;height:%s'/>", uri, di.X().Extent.Size().Width, di.X().Extent.Size().Height)
				}
				//	公式数据
			} else if r.OleObjects() != nil {
				for _, ole := range r.OleObjects() {
					latex := w.oles[ole.OleRid()]
					text = *latex
				}
				//	文本数据
			} else {
				text = r.Text()
			}

			resText.WriteString(text)
			htmlResText.WriteString(text)
		}

		//新的段落换行
		if paragIdx < len(paragraphs)-1 {
			htmlResText.WriteString("<br/>")
		}
	}

	return resText.String(), htmlResText.String()
}

//把ole对象文件转为latex字符串
func (w *Word) parseOle(olePaths []document.OleObjectPath) {
	w.oles = make(map[string]*string)

	//使用 WaitGroup 来跟踪 goroutine 的工作是否完成
	var wg sync.WaitGroup
	wg.Add(len(olePaths))

	//循环数据
	for _, ole := range olePaths {
		//goroutine 运行
		go func(word *Word, oleObjPath document.OleObjectPath) {
			// 在函数退出时调用 Done
			defer wg.Done()

			if _, ok := word.oles[oleObjPath.Rid()]; !ok {
				//调用解析库解析公式
				latex := eqn.Convert(oleObjPath.Path())

				//替换$$为[ 或 ]
				latex = strings.Replace(latex, "$$", "[", 1)
				latex = strings.Replace(latex, "$$", "]", 1)

				//保存数据
				word.oles[oleObjPath.Rid()] = &latex
			}
		}(w, ole)
	}

	wg.Wait()
}

//把图片上传到七牛
func (w *Word) parseImage(images []common.ImageRef) {
	w.images = make(map[string]string)

	//使用 WaitGroup 来跟踪 goroutine 的工作是否完成
	var wg sync.WaitGroup
	wg.Add(len(images))

	for _, img := range images {
		//goroutine 运行
		go func(word *Word, image common.ImageRef) {
			// 在函数退出时调用 Done
			defer wg.Done()

			if _, ok := word.images[image.RelID()]; !ok {
				//调用图片上传
				localFile := image.Path()
				key := fmt.Sprintf("%s.%s", strconv.Itoa(int(time.Now().UnixNano())), image.Format())

				//上传到七牛
				uri := utils.UploadFileToQiniu(key, localFile)
				word.images[image.RelID()] = uri
			}
		}(w, img)
	}

	wg.Wait()
}
