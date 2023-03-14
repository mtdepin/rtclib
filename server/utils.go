package server

import (
	"bufio"
	"fmt"
	"github.com/pion/ice/v2"
	"os"
	"strconv"
)

func GenCandidateStatsString(stats []ice.CandidateStats) string {
	candiStrs := ""
	for _, candi := range stats {
		candiStrs += fmt.Sprintf("id: %s network:%s addr:%s type:%s priority:%d\n",
			candi.ID,
			candi.NetworkType.String(),
			candi.IP+":"+strconv.Itoa(candi.Port),
			candi.CandidateType.String(),
			candi.Priority,
		)
	}
	return candiStrs
}

func GenCandidatePairsStatsString(stats []ice.CandidatePairStats) string {
	candiPairsStrs := ""
	for _, candiPair := range stats {
		candiPairsStrs += fmt.Sprintf("local:%s remote:%s state:%s\n",
			candiPair.LocalCandidateID,
			candiPair.RemoteCandidateID,
			candiPair.State.String())
	}

	return candiPairsStrs
}

func checkFileIsExist(filename string) bool {
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return false
	}
	return true
}

func WriteFile(fileName string, data []byte) error {
	var file *os.File
	var err error

	if checkFileIsExist(fileName) { //如果文件存在
		file, err = os.OpenFile(fileName, os.O_WRONLY|os.O_APPEND, 0666) //打开文件
		//fmt.Println("文件存在")
	} else {
		file, err = os.Create(fileName) //创建文件
		fmt.Println("文件不存在")
	}

	if err != nil {
		fmt.Println("文件打开失败", err)
		return err
	}
	defer file.Close()
	//写入文件时，使用带缓存的 *Writer
	write := bufio.NewWriter(file)
	if _, err = write.Write(data); err != nil {
		return err
	}
	//Flush将缓存的文件真正写入到文件中
	if err = write.Flush(); err != nil {
		return err
	}
	return nil
}
